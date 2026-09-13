package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	stdsync "sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// ErrReenrollRequired is returned by Run when the control plane directs
// the workload to re-enroll. The daemon cannot do that itself: enrollment
// needs a provisioning token delivered out of band (ADR-0016).
var ErrReenrollRequired = errors.New("sync: control plane directed re-enrollment; enroll again with a new provisioning token")

// Dialer opens a connection to the control plane. The default dials gRPC
// over the holder's TLS configuration; tests substitute a loopback.
type Dialer func(ctx context.Context, server string, holder *credential.Holder) (innerwallv1.AgentServiceClient, io.Closer, error)

// Config configures a Daemon.
type Config struct {
	// Server is the control-plane address, host:port.
	Server string
	// Holder presents the workload credential.
	Holder *credential.Holder
	// Store receives each complete policy.
	Store enforce.PolicyStore
	// AgentVersion and Capabilities are reported in Hello.
	AgentVersion string
	Capabilities []string
	// Facts and ListeningServices are collected for Hello and for every
	// inventory report. Either may be nil.
	Facts             func() *innerwallv1.HostFacts
	ListeningServices func() []*innerwallv1.ListeningService
	Log               *slog.Logger
	// BackoffBase and BackoffCap bound the reconnect wait: one second and
	// five minutes when zero.
	BackoffBase time.Duration
	BackoffCap  time.Duration
	// HelloTimeout bounds the wait for HelloAck; 30 seconds when zero.
	HelloTimeout time.Duration
	// Dial replaces the default dialer.
	Dial Dialer
	// Uptime reports the daemon's uptime for heartbeats; measured from
	// New when nil.
	Uptime func() time.Duration
	// DroppedFlowRecords reports the flow records the collection loop has
	// dropped to buffer pressure, carried on every heartbeat. Zero when nil.
	DroppedFlowRecords func() uint64
	// RenewalError reports the reason the last automatic credential
	// renewal failed, or empty, carried on every heartbeat. Empty when nil.
	RenewalError func() string
	// OnSyncConfig is called with the configuration in every HelloAck, so
	// the other loops adopt the control plane's parameters.
	OnSyncConfig func(*innerwallv1.SyncConfig)
}

// Daemon is the agent's sync loop.
type Daemon struct {
	cfg     Config
	log     *slog.Logger
	rng     *rand.Rand
	started time.Time
}

// New builds a daemon.
func New(cfg Config) *Daemon {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = time.Second
	}
	if cfg.BackoffCap <= 0 {
		cfg.BackoffCap = 5 * time.Minute
	}
	if cfg.HelloTimeout <= 0 {
		cfg.HelloTimeout = 30 * time.Second
	}
	if cfg.Dial == nil {
		cfg.Dial = DialGRPC
	}
	return &Daemon{
		cfg:     cfg,
		log:     cfg.Log,
		rng:     rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1)), //nolint:gosec // jitter, not secrecy
		started: time.Now(),
	}
}

// DialGRPC is the production dialer: mutual TLS with the holder's
// current credential, refreshed at each handshake. The flow reporter dials
// with it too, on its own connection.
func DialGRPC(_ context.Context, server string, holder *credential.Holder) (innerwallv1.AgentServiceClient, io.Closer, error) {
	tlsCfg, err := holder.TLSConfig()
	if err != nil {
		return nil, nil, err
	}
	conn, err := grpc.NewClient(server, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return nil, nil, fmt.Errorf("sync: dialing %s: %w", server, err)
	}
	return innerwallv1.NewAgentServiceClient(conn), conn, nil
}

// Backoff returns the wait before reconnect attempt number attempt (1 for
// the first retry): a random duration in [0, min(cap, base × 2^attempt)],
// so that a control-plane restart produces a smooth trickle of handshakes
// rather than a synchronized wave (ADR-0002).
func Backoff(attempt int, base, limit time.Duration, rng *rand.Rand) time.Duration {
	ceiling := base << uint(min(attempt, 20)) //nolint:gosec // bounded
	if ceiling > limit || ceiling <= 0 {
		ceiling = limit
	}
	return time.Duration(rng.Int64N(int64(ceiling)))
}

// Run maintains the stream until ctx ends, reconnecting with backoff after
// every failure. Every reconnect is a fresh Hello and snapshot. It returns
// ErrReenrollRequired when the control plane says so; every other stream
// error is a reason to reconnect, not to stop.
func (d *Daemon) Run(ctx context.Context) error {
	attempt := 0
	for {
		err := d.runSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, ErrReenrollRequired) {
			return err
		}
		attempt++
		wait := Backoff(attempt, d.cfg.BackoffBase, d.cfg.BackoffCap, d.rng)
		if err != nil {
			d.log.Warn("sync stream ended; reconnecting", "error", err, "attempt", attempt, "wait", wait)
		} else {
			d.log.Info("sync stream closed by control plane; reconnecting", "attempt", attempt, "wait", wait)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// send serializes writes to the stream, which are otherwise not safe from
// more than one goroutine.
type sender struct {
	mu     stdsync.Mutex
	stream innerwallv1.AgentService_SyncClient
}

func (s *sender) send(msg *innerwallv1.SyncRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stream.Send(msg)
}

// runSession runs one stream from dial to close.
func (d *Daemon) runSession(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client, closer, err := d.cfg.Dial(ctx, d.cfg.Server, d.cfg.Holder)
	if err != nil {
		return err
	}
	defer func() { _ = closer.Close() }()

	stream, err := client.Sync(ctx)
	if err != nil {
		return fmt.Errorf("sync: opening stream: %w", err)
	}
	out := &sender{stream: stream}

	applied := uint64(0)
	if cur := d.cfg.Store.Current(); cur != nil {
		applied = cur.GetVersion()
	}
	var facts *innerwallv1.HostFacts
	if d.cfg.Facts != nil {
		facts = d.cfg.Facts()
	}
	if err := out.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Hello{Hello: &innerwallv1.Hello{
		Agent:                &innerwallv1.AgentInfo{Version: d.cfg.AgentVersion, Capabilities: d.cfg.Capabilities},
		AppliedPolicyVersion: applied,
		Facts:                facts,
	}}}); err != nil {
		return fmt.Errorf("sync: sending hello: %w", err)
	}

	// The reader goroutine owns Recv and applies updates inline, so
	// application is strictly in stream order.
	type inbound struct {
		msg *innerwallv1.SyncResponse
		err error
	}
	msgs := make(chan inbound, 1)
	go func() {
		for {
			m, err := stream.Recv()
			msgs <- inbound{m, err}
			if err != nil {
				return
			}
		}
	}()

	// HelloAck first, within the timeout.
	var cfg *innerwallv1.SyncConfig
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(d.cfg.HelloTimeout):
		return errors.New("sync: no HelloAck within the timeout")
	case in := <-msgs:
		if in.err != nil {
			return fmt.Errorf("sync: waiting for HelloAck: %w", in.err)
		}
		ack := in.msg.GetHelloAck()
		if ack == nil {
			return fmt.Errorf("sync: expected HelloAck, got %T", in.msg.GetMsg())
		}
		cfg = ack.GetConfig()
	}
	d.log.Info("sync stream established", "server", d.cfg.Server, "heartbeat_interval", cfg.GetHeartbeatIntervalSeconds(), "inventory_interval", cfg.GetInventoryReportIntervalSeconds(), "flow_window", cfg.GetFlowAggregationWindowSeconds(), "flow_batch_max", cfg.GetFlowBatchMaxRecords())
	if d.cfg.OnSyncConfig != nil {
		d.cfg.OnSyncConfig(cfg)
	}

	heartbeat := newTicker(cfg.GetHeartbeatIntervalSeconds())
	defer heartbeat.Stop()
	inventory := newTicker(cfg.GetInventoryReportIntervalSeconds())
	defer inventory.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case in := <-msgs:
			if in.err != nil {
				return in.err
			}
			if err := d.handle(ctx, out, in.msg); err != nil {
				return err
			}
		case <-heartbeat.C:
			if err := out.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Heartbeat{Heartbeat: &innerwallv1.Heartbeat{
				UptimeSeconds:          uint64(d.uptime().Seconds()),
				DroppedFlowRecords:     d.dropped(),
				CredentialRenewalError: d.renewalError(),
			}}}); err != nil {
				return fmt.Errorf("sync: sending heartbeat: %w", err)
			}
		case <-inventory.C:
			report := &innerwallv1.InventoryReport{}
			if d.cfg.Facts != nil {
				report.Facts = d.cfg.Facts()
			}
			if d.cfg.ListeningServices != nil {
				report.ListeningServices = d.cfg.ListeningServices()
			}
			if err := out.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Inventory{Inventory: report}}); err != nil {
				return fmt.Errorf("sync: sending inventory: %w", err)
			}
		}
	}
}

func (d *Daemon) dropped() uint64 {
	if d.cfg.DroppedFlowRecords != nil {
		return d.cfg.DroppedFlowRecords()
	}
	return 0
}

func (d *Daemon) renewalError() string {
	if d.cfg.RenewalError != nil {
		return d.cfg.RenewalError()
	}
	return ""
}

func (d *Daemon) uptime() time.Duration {
	if d.cfg.Uptime != nil {
		return d.cfg.Uptime()
	}
	return time.Since(d.started)
}

// newTicker returns a ticker for an interval in seconds; zero disables it
// (a ticker that never fires).
func newTicker(seconds uint32) *time.Ticker {
	if seconds == 0 {
		t := time.NewTicker(time.Hour)
		t.Stop()
		return t
	}
	return time.NewTicker(time.Duration(seconds) * time.Second)
}

// handle processes one server message.
func (d *Daemon) handle(ctx context.Context, out *sender, msg *innerwallv1.SyncResponse) error {
	switch m := msg.GetMsg().(type) {
	case *innerwallv1.SyncResponse_PolicyUpdate:
		return d.applyUpdate(ctx, out, m.PolicyUpdate)
	case *innerwallv1.SyncResponse_Directive:
		switch m.Directive.GetDirective().(type) {
		case *innerwallv1.Directive_Reconnect:
			d.log.Info("control plane directed a reconnect")
			return errors.New("sync: reconnect directed")
		case *innerwallv1.Directive_Reenroll:
			d.log.Error("control plane directed re-enrollment; this daemon cannot enroll itself")
			return ErrReenrollRequired
		default:
			d.log.Warn("ignoring unknown directive")
			return nil
		}
	case *innerwallv1.SyncResponse_HelloAck:
		return errors.New("sync: unexpected second HelloAck")
	default:
		d.log.Warn("ignoring empty sync message")
		return nil
	}
}

// applyUpdate turns an update into the next complete policy, installs it
// atomically, and acknowledges. A delta that does not apply to the current
// version, or a store that refuses the policy, is acknowledged FAILED with
// the reason, and the installed policy is untouched.
func (d *Daemon) applyUpdate(ctx context.Context, out *sender, update *innerwallv1.PolicyUpdate) error {
	current := d.cfg.Store.Current()
	if current == nil {
		current = rendered.Empty()
	}
	var (
		next    *innerwallv1.WorkloadPolicy
		version uint64
		kind    string
		err     error
	)
	switch u := update.GetUpdate().(type) {
	case *innerwallv1.PolicyUpdate_Snapshot:
		kind = "snapshot"
		next = rendered.Canonical(u.Snapshot.GetPolicy())
		version = next.GetVersion()
	case *innerwallv1.PolicyUpdate_Delta:
		kind = "delta"
		version = u.Delta.GetToVersion()
		next, err = rendered.Apply(current, u.Delta)
	default:
		return errors.New("sync: policy update carries neither snapshot nor delta")
	}
	if err == nil {
		err = d.cfg.Store.Apply(ctx, next)
	}
	if err != nil {
		d.log.Error("policy apply failed; staying on last good version", "kind", kind, "version", version, "current_version", current.GetVersion(), "error", err)
		return out.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_PolicyAck{PolicyAck: &innerwallv1.PolicyAck{
			Version: version, Status: innerwallv1.AckStatus_ACK_STATUS_FAILED, ErrorDetail: err.Error(),
		}}})
	}
	// Every applied change is logged, for a snapshot as the difference
	// from what was installed before it.
	changes := rendered.Diff(current, next)
	for _, ch := range changes {
		d.log.Info("policy change applied", "version", version, "change", rendered.Describe(ch))
	}
	d.log.Info("policy applied", "kind", kind, "from_version", current.GetVersion(), "version", version, "changes", len(changes), "rules", len(next.GetInboundRules()), "mode", next.GetMode())
	return out.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_PolicyAck{PolicyAck: &innerwallv1.PolicyAck{
		Version: version, Status: innerwallv1.AckStatus_ACK_STATUS_APPLIED,
	}}})
}
