package sync_test

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// fakeServer is a scriptable control plane: each Sync stream is handed to
// the test as a conn, and the test drives it message by message.
type fakeServer struct {
	innerwallv1.UnimplementedAgentServiceServer
	conns chan *conn
}

type conn struct {
	stream innerwallv1.AgentService_SyncServer
	in     chan *innerwallv1.SyncRequest
	closed chan struct{}
	once   sync.Once
}

func (f *fakeServer) Sync(stream innerwallv1.AgentService_SyncServer) error {
	c := &conn{stream: stream, in: make(chan *innerwallv1.SyncRequest, 64), closed: make(chan struct{})}
	go func() {
		for {
			m, err := stream.Recv()
			if err != nil {
				c.close()
				return
			}
			c.in <- m
		}
	}()
	f.conns <- c
	select {
	case <-c.closed:
	case <-stream.Context().Done():
	}
	return nil
}

func (c *conn) close() { c.once.Do(func() { close(c.closed) }) }

func (c *conn) send(t *testing.T, msg *innerwallv1.SyncResponse) {
	t.Helper()
	if err := c.stream.Send(msg); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func (c *conn) expect(t *testing.T) *innerwallv1.SyncRequest {
	t.Helper()
	select {
	case m := <-c.in:
		return m
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for an agent message")
		return nil
	}
}

func (c *conn) expectAck(t *testing.T, version uint64, st innerwallv1.AckStatus) *innerwallv1.PolicyAck {
	t.Helper()
	for {
		m := c.expect(t)
		ack := m.GetPolicyAck()
		if ack == nil {
			// Heartbeats and inventory may interleave; they are not what
			// this expectation is about.
			continue
		}
		if ack.GetVersion() != version || ack.GetStatus() != st {
			t.Fatalf("ack = %v, want version %d %v", ack, version, st)
		}
		return ack
	}
}

func helloAck(hb, inv uint32) *innerwallv1.SyncResponse {
	return &innerwallv1.SyncResponse{Msg: &innerwallv1.SyncResponse_HelloAck{HelloAck: &innerwallv1.HelloAck{Config: &innerwallv1.SyncConfig{HeartbeatIntervalSeconds: hb, InventoryReportIntervalSeconds: inv}}}}
}

func snapshot(p *innerwallv1.WorkloadPolicy) *innerwallv1.SyncResponse {
	return &innerwallv1.SyncResponse{Msg: &innerwallv1.SyncResponse_PolicyUpdate{PolicyUpdate: &innerwallv1.PolicyUpdate{Update: &innerwallv1.PolicyUpdate_Snapshot{Snapshot: &innerwallv1.PolicySnapshot{Policy: p}}}}}
}

func delta(d *innerwallv1.PolicyDelta) *innerwallv1.SyncResponse {
	return &innerwallv1.SyncResponse{Msg: &innerwallv1.SyncResponse_PolicyUpdate{PolicyUpdate: &innerwallv1.PolicyUpdate{Update: &innerwallv1.PolicyUpdate_Delta{Delta: d}}}}
}

func policy(v uint64, rules ...*innerwallv1.ResolvedRule) *innerwallv1.WorkloadPolicy {
	return &innerwallv1.WorkloadPolicy{Version: v, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, InboundRules: rules}
}

func rule(id string, peers ...string) *innerwallv1.ResolvedRule {
	return &innerwallv1.ResolvedRule{RuleId: id, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: peers, Ports: []*innerwallv1.PortRange{{Start: 22, End: 22}}}
}

// startServer runs the fake control plane on a loopback listener without
// TLS; the daemon's dialer is substituted to match. Credentials are the
// gateway's concern and are tested there.
func startServer(t *testing.T) (*fakeServer, agentsync.Dialer) {
	t.Helper()
	srv := &fakeServer{conns: make(chan *conn, 8)}
	g := grpc.NewServer()
	innerwallv1.RegisterAgentServiceServer(g, srv)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = g.Serve(lis) }()
	t.Cleanup(g.Stop)
	addr := lis.Addr().String()
	dial := func(context.Context, string, *credential.Holder) (innerwallv1.AgentServiceClient, io.Closer, error) {
		cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return innerwallv1.NewAgentServiceClient(cc), cc, nil
	}
	return srv, dial
}

func startDaemon(t *testing.T, cfg agentsync.Config) (*enforce.MemoryStore, <-chan error) {
	t.Helper()
	store := &enforce.MemoryStore{}
	cfg.Store = store
	cfg.AgentVersion = "test"
	if cfg.BackoffBase == 0 {
		cfg.BackoffBase = time.Millisecond
		cfg.BackoffCap = 20 * time.Millisecond
	}
	d := agentsync.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	t.Cleanup(cancel)
	return store, done
}

func TestAppliesInOrderAndAcksEach(t *testing.T) {
	srv, dial := startServer(t)
	store, _ := startDaemon(t, agentsync.Config{Dial: dial, Facts: func() *innerwallv1.HostFacts { return &innerwallv1.HostFacts{Hostname: "h"} }})
	c := <-srv.conns
	hello := c.expect(t).GetHello()
	if hello == nil || hello.GetAppliedPolicyVersion() != 0 || hello.GetFacts().GetHostname() != "h" || hello.GetAgent().GetVersion() != "test" {
		t.Fatalf("hello = %v", hello)
	}
	c.send(t, helloAck(0, 0))

	v1 := policy(1, rule("a/tcp", "10.0.0.1/32"))
	v2 := policy(2, rule("a/tcp", "10.0.0.1/32", "10.0.0.2/32"))
	v3 := policy(3, rule("a/tcp", "10.0.0.2/32"), rule("b/tcp", "0.0.0.0/0"))
	// Pipelined: snapshot and two deltas without waiting for acks.
	c.send(t, snapshot(v1))
	c.send(t, delta(rendered.Delta(v1, v2)))
	c.send(t, delta(rendered.Delta(v2, v3)))
	c.expectAck(t, 1, innerwallv1.AckStatus_ACK_STATUS_APPLIED)
	c.expectAck(t, 2, innerwallv1.AckStatus_ACK_STATUS_APPLIED)
	c.expectAck(t, 3, innerwallv1.AckStatus_ACK_STATUS_APPLIED)
	if cur := store.Current(); cur.GetVersion() != 3 || !rendered.Equal(cur, v3) {
		t.Fatalf("installed = %v, want v3", cur)
	}
}

func TestFailedApplyKeepsLastGoodAndAcksFailed(t *testing.T) {
	srv, dial := startServer(t)
	store, _ := startDaemon(t, agentsync.Config{Dial: dial})
	store.Fail = func(p *innerwallv1.WorkloadPolicy) error {
		if p.GetVersion() == 2 {
			return errors.New("simulated firewall refusal")
		}
		return nil
	}
	c := <-srv.conns
	c.expect(t)
	c.send(t, helloAck(0, 0))
	v1 := policy(1, rule("a/tcp", "10.0.0.1/32"))
	v2 := policy(2, rule("a/tcp", "10.0.0.9/32"))
	c.send(t, snapshot(v1))
	c.expectAck(t, 1, innerwallv1.AckStatus_ACK_STATUS_APPLIED)

	// The store refuses v2: FAILED with the reason, still on v1.
	c.send(t, delta(rendered.Delta(v1, v2)))
	ack := c.expectAck(t, 2, innerwallv1.AckStatus_ACK_STATUS_FAILED)
	if ack.GetErrorDetail() != "simulated firewall refusal" {
		t.Fatalf("detail = %q", ack.GetErrorDetail())
	}
	if cur := store.Current(); cur.GetVersion() != 1 || !rendered.Equal(cur, v1) {
		t.Fatalf("installed = %v, want v1 untouched", cur)
	}
	// A delta against a base the agent does not hold: FAILED, untouched.
	c.send(t, delta(&innerwallv1.PolicyDelta{FromVersion: 5, ToVersion: 6}))
	ack = c.expectAck(t, 6, innerwallv1.AckStatus_ACK_STATUS_FAILED)
	if !strings.Contains(ack.GetErrorDetail(), "base version") {
		t.Fatalf("detail = %q", ack.GetErrorDetail())
	}
	if cur := store.Current(); cur.GetVersion() != 1 {
		t.Fatalf("installed version = %d after base mismatch", cur.GetVersion())
	}
	// The recovery snapshot applies.
	v3 := policy(3, rule("a/tcp", "10.0.0.9/32"))
	c.send(t, snapshot(v3))
	c.expectAck(t, 3, innerwallv1.AckStatus_ACK_STATUS_APPLIED)
	if cur := store.Current(); cur.GetVersion() != 3 {
		t.Fatalf("installed version = %d, want 3", cur.GetVersion())
	}
}

func TestReconnectIsFreshHelloWithBackoff(t *testing.T) {
	srv, realDial := startServer(t)
	var attempts atomic.Int32
	failures := int32(3)
	dial := func(ctx context.Context, server string, h *credential.Holder) (innerwallv1.AgentServiceClient, io.Closer, error) {
		if attempts.Add(1) <= failures {
			return nil, nil, errors.New("connection refused")
		}
		return realDial(ctx, server, h)
	}
	store, _ := startDaemon(t, agentsync.Config{Dial: dial})
	c := <-srv.conns
	if got := attempts.Load(); got != failures+1 {
		t.Fatalf("dial attempts = %d, want %d", got, failures+1)
	}
	c.expect(t)
	c.send(t, helloAck(0, 0))
	c.send(t, snapshot(policy(4, rule("a/tcp", "10.0.0.1/32"))))
	c.expectAck(t, 4, innerwallv1.AckStatus_ACK_STATUS_APPLIED)

	// The control plane drops the stream; the daemon comes back with a
	// fresh Hello reporting what it has applied, and takes a snapshot
	// again (the contract: reconnects never resume with deltas).
	c.close()
	c2 := <-srv.conns
	hello := c2.expect(t).GetHello()
	if hello == nil || hello.GetAppliedPolicyVersion() != 4 {
		t.Fatalf("second hello = %v", hello)
	}
	c2.send(t, helloAck(0, 0))
	c2.send(t, snapshot(policy(5)))
	c2.expectAck(t, 5, innerwallv1.AckStatus_ACK_STATUS_APPLIED)
	if store.Current().GetVersion() != 5 {
		t.Fatalf("installed version = %d", store.Current().GetVersion())
	}
}

func TestBackoffIsBoundedWithFullJitter(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // test
	base, limit := time.Second, time.Minute
	for attempt := 1; attempt <= 12; attempt++ {
		ceiling := min(base<<uint(attempt), limit) //nolint:gosec // small
		sawSpread := false
		var first time.Duration
		for i := range 200 {
			w := agentsync.Backoff(attempt, base, limit, rng)
			if w < 0 || w >= ceiling {
				t.Fatalf("attempt %d: wait %v outside [0, %v)", attempt, w, ceiling)
			}
			if i == 0 {
				first = w
			} else if w != first {
				sawSpread = true
			}
		}
		if !sawSpread {
			t.Fatalf("attempt %d: no jitter", attempt)
		}
	}
}

func TestHeartbeatAndInventoryFollowConfig(t *testing.T) {
	srv, dial := startServer(t)
	facts := &innerwallv1.HostFacts{Hostname: "h", Interfaces: []*innerwallv1.NetworkInterface{{Name: "eth0", Addresses: []string{"10.0.0.7/24"}}}}
	startDaemon(t, agentsync.Config{
		Dial:  dial,
		Facts: func() *innerwallv1.HostFacts { return facts },
		ListeningServices: func() []*innerwallv1.ListeningService {
			return []*innerwallv1.ListeningService{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Port: 22}}
		},
		Uptime: func() time.Duration { return 42 * time.Second },
	})
	c := <-srv.conns
	c.expect(t)
	c.send(t, helloAck(1, 1))
	c.send(t, snapshot(policy(1)))
	c.expectAck(t, 1, innerwallv1.AckStatus_ACK_STATUS_APPLIED)
	var sawHeartbeat, sawInventory bool
	deadline := time.After(10 * time.Second)
	for !sawHeartbeat || !sawInventory {
		select {
		case m := <-c.in:
			if hb := m.GetHeartbeat(); hb != nil {
				if hb.GetUptimeSeconds() != 42 || hb.GetDroppedFlowRecords() != 0 {
					t.Fatalf("heartbeat = %v", hb)
				}
				sawHeartbeat = true
			}
			if inv := m.GetInventory(); inv != nil {
				if inv.GetFacts().GetHostname() != "h" || len(inv.GetListeningServices()) != 1 || inv.GetListeningServices()[0].GetPort() != 22 {
					t.Fatalf("inventory = %v", inv)
				}
				sawInventory = true
			}
		case <-deadline:
			t.Fatalf("heartbeat=%v inventory=%v after deadline", sawHeartbeat, sawInventory)
		}
	}
}

func TestReenrollDirectiveStopsTheDaemon(t *testing.T) {
	srv, dial := startServer(t)
	_, done := startDaemon(t, agentsync.Config{Dial: dial})
	c := <-srv.conns
	c.expect(t)
	c.send(t, helloAck(0, 0))
	c.send(t, &innerwallv1.SyncResponse{Msg: &innerwallv1.SyncResponse_Directive{Directive: &innerwallv1.Directive{Directive: &innerwallv1.Directive_Reenroll{Reenroll: &innerwallv1.Reenroll{}}}}})
	select {
	case err := <-done:
		if !errors.Is(err, agentsync.ErrReenrollRequired) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon kept running after a re-enroll directive")
	}
}
