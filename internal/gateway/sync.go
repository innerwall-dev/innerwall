package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// DefaultSyncConfig is what the control plane hands every agent unless the
// operator configures otherwise. Centralizing these means fleet-wide tuning
// without an agent redeployment.
func DefaultSyncConfig() *innerwallv1.SyncConfig {
	return &innerwallv1.SyncConfig{
		HeartbeatIntervalSeconds:       30,
		InventoryReportIntervalSeconds: 300,
		FlowAggregationWindowSeconds:   60,
		FlowBatchMaxRecords:            1000,
	}
}

// sendQueueDepth bounds the pipelined updates queued to one stream. Policy
// traffic is low volume; the bound exists so an agent that stops reading
// cannot make the control plane retain unbounded state on its behalf.
const sendQueueDepth = 64

// offlineWriteTimeout bounds the status write that runs after a stream's
// own context has already ended.
const offlineWriteTimeout = 5 * time.Second

// session is one live sync stream: the in-process record that lets a
// render's announcement find the stream to push to. It holds only stream
// state: what was last sent on this stream. Everything durable is in the
// registry (ADR-0017).
type session struct {
	id     identity.WorkloadID
	ctx    context.Context
	cancel context.CancelCauseFunc
	send   chan *innerwallv1.SyncResponse

	mu sync.Mutex
	// lastSent is the tip of what has been sent on this stream: the
	// snapshot at open, then every delta's target. The next delta is
	// computed from it, so pushes coalesce for free when the stream lags.
	lastSent *innerwallv1.WorkloadPolicy
	// recovery is the version of the last recovery snapshot sent after a
	// FAILED ack. Failed acks for versions at or below it are stale
	// deltas that were already in flight and need no second snapshot.
	recovery uint64
}

// enqueue queues one message for the writer, preserving order under the
// session lock, and gives up when the session ends.
func (s *session) enqueue(msg *innerwallv1.SyncResponse) bool {
	select {
	case s.send <- msg:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// errSuperseded ends a stream when the same workload opens a newer one.
var errSuperseded = errors.New("superseded by a newer stream from the same workload")

// sessions is the in-process connection registry: workload to live stream.
type sessions struct {
	mu   sync.Mutex
	live map[identity.WorkloadID]*session
}

// add registers sess, ending any earlier stream of the same workload. Two
// live streams for one identity would each believe they hold the tip; the
// newer wins because it is the one whose Hello the control plane just
// answered.
func (r *sessions) add(sess *session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.live[sess.id]; ok {
		old.cancel(errSuperseded)
	}
	r.live[sess.id] = sess
}

// remove drops sess if it is still the registered stream for its workload.
// It reports whether it was, so a superseded stream does not mark a
// workload offline while its replacement is live.
func (r *sessions) remove(sess *session) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live[sess.id] != sess {
		return false
	}
	delete(r.live, sess.id)
	return true
}

func (r *sessions) get(id identity.WorkloadID) *session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.live[id]
}

func (r *sessions) all() []*session {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*session, 0, len(r.live))
	for _, s := range r.live {
		out = append(out, s)
	}
	return out
}

// Sync implements the desired-state stream (ADR-0002, ADR-0015): Hello,
// then HelloAck, then an unconditional snapshot, then deltas pushed as
// renders announce them, with acknowledgements, inventory, and heartbeats
// flowing up on the same stream.
func (s *Server) Sync(stream innerwallv1.AgentService_SyncServer) error {
	ctx := stream.Context()
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "workload credential required")
	}
	if s.registry == nil || s.policies == nil || s.engine == nil {
		return status.Error(codes.FailedPrecondition, "this control plane serves enrollment only")
	}
	log := s.log.With("workload_id", id)

	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "the first message on a sync stream must be Hello")
	}
	now := s.now()
	agent := registry.AgentInfo{Version: hello.GetAgent().GetVersion(), Capabilities: hello.GetAgent().GetCapabilities()}
	if err := s.registry.RecordAgent(ctx, id, agent, hello.GetAppliedPolicyVersion(), now); err != nil {
		if errors.Is(err, registry.ErrWorkloadUnknown) {
			// A valid credential for a workload the registry does not
			// hold: the identity was granted but its record is gone.
			return status.Error(codes.PermissionDenied, registry.ErrWorkloadUnknown.Error())
		}
		log.Error("recording agent", "error", err)
		return status.Error(codes.Internal, "recording agent")
	}
	factsChanged := false
	if hello.GetFacts() != nil {
		changed, err := s.registry.RecordFacts(ctx, id, hello.GetFacts(), now)
		if err != nil {
			log.Error("recording facts", "error", err)
			return status.Error(codes.Internal, "recording facts")
		}
		factsChanged = changed
	}

	sctx, cancel := context.WithCancelCause(ctx)
	sess := &session{id: id, ctx: sctx, cancel: cancel, send: make(chan *innerwallv1.SyncResponse, sendQueueDepth)}
	s.sessions.add(sess)
	defer func() {
		cancel(nil)
		if s.sessions.remove(sess) {
			octx, ocancel := context.WithTimeout(context.Background(), offlineWriteTimeout)
			defer ocancel()
			if err := s.registry.SetSyncState(octx, id, innerwallv1.SyncState_SYNC_STATE_OFFLINE, "", s.now()); err != nil {
				log.Error("marking workload offline", "error", err)
			}
		}
	}()

	// Writer: the only goroutine that calls Send.
	writeErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-sctx.Done():
				writeErr <- nil
				return
			case msg := <-sess.send:
				if err := stream.Send(msg); err != nil {
					cancel(err)
					writeErr <- err
					return
				}
			}
		}
	}()

	sess.enqueue(&innerwallv1.SyncResponse{Msg: &innerwallv1.SyncResponse_HelloAck{HelloAck: &innerwallv1.HelloAck{Config: s.syncConfig}}})

	// Facts that changed the workload's addresses are a render trigger:
	// its peers' policies resolve to those addresses. Rendering before the
	// snapshot means the snapshot already reflects them.
	if factsChanged {
		if _, err := s.engine.Render(ctx); err != nil {
			log.Error("rendering after facts change", "error", err)
		}
	}
	policy, err := s.currentPolicy(ctx, id)
	if err != nil {
		log.Error("loading policy for snapshot", "error", err)
		return status.Error(codes.Internal, "loading policy")
	}
	sess.mu.Lock()
	sess.lastSent = policy
	sess.mu.Unlock()
	sess.enqueue(&innerwallv1.SyncResponse{Msg: &innerwallv1.SyncResponse_PolicyUpdate{PolicyUpdate: &innerwallv1.PolicyUpdate{Update: &innerwallv1.PolicyUpdate_Snapshot{Snapshot: &innerwallv1.PolicySnapshot{Policy: policy}}}}})
	if err := s.registry.SetSyncState(ctx, id, innerwallv1.SyncState_SYNC_STATE_PENDING, "", now); err != nil {
		log.Error("recording sync state", "error", err)
	}
	log.Info("sync stream opened", "agent_version", agent.Version, "claimed_version", hello.GetAppliedPolicyVersion(), "snapshot_version", policy.GetVersion())

	// Reader loop.
	recvErr := make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			if err := s.handleMessage(sctx, log, sess, msg); err != nil {
				recvErr <- err
				return
			}
		}
	}()

	select {
	case err := <-recvErr:
		if errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
			return nil
		}
		if err != nil && !isEOF(err) {
			log.Info("sync stream closed", "error", err)
			return err
		}
		log.Info("sync stream closed")
		return nil
	case err := <-writeErr:
		if cause := context.Cause(sctx); errors.Is(cause, errSuperseded) {
			log.Info("sync stream superseded")
			return status.Error(codes.Aborted, errSuperseded.Error())
		}
		return err
	case <-sctx.Done():
		if cause := context.Cause(sctx); errors.Is(cause, errSuperseded) {
			log.Info("sync stream superseded")
			return status.Error(codes.Aborted, errSuperseded.Error())
		}
		return nil
	}
}

func isEOF(err error) bool {
	return err != nil && err.Error() == "EOF"
}

// handleMessage dispatches one agent message after Hello.
func (s *Server) handleMessage(ctx context.Context, log *slog.Logger, sess *session, msg *innerwallv1.SyncRequest) error {
	now := s.now()
	switch m := msg.GetMsg().(type) {
	case *innerwallv1.SyncRequest_PolicyAck:
		return s.handleAck(ctx, log, sess, m.PolicyAck, now)
	case *innerwallv1.SyncRequest_Inventory:
		changed, err := s.registry.RecordFacts(ctx, sess.id, m.Inventory.GetFacts(), now)
		if err != nil {
			return status.Error(codes.Internal, "recording facts")
		}
		services := make([]registry.ListeningService, 0, len(m.Inventory.GetListeningServices()))
		for _, ls := range m.Inventory.GetListeningServices() {
			services = append(services, registry.ListeningService{Protocol: ls.GetProtocol(), Port: ls.GetPort(), ProcessName: ls.GetProcessName(), ProcessPath: ls.GetProcessPath()})
		}
		if err := s.registry.RecordListeningServices(ctx, sess.id, services, now); err != nil {
			return status.Error(codes.Internal, "recording listening services")
		}
		if changed {
			log.Info("workload addresses changed; rendering")
			if _, err := s.engine.Render(ctx); err != nil {
				log.Error("rendering after inventory change", "error", err)
			}
		}
		return nil
	case *innerwallv1.SyncRequest_Heartbeat:
		if err := s.registry.RecordHeartbeat(ctx, sess.id, m.Heartbeat.GetDroppedFlowRecords(), now); err != nil {
			return status.Error(codes.Internal, "recording heartbeat")
		}
		return nil
	case *innerwallv1.SyncRequest_Hello:
		return status.Error(codes.InvalidArgument, "Hello is only valid as the first message of a stream")
	default:
		return status.Error(codes.InvalidArgument, "empty sync message")
	}
}

// handleAck records an acknowledgement. APPLIED moves the workload to
// SYNCED when it names the tip of what was sent and PENDING while more is
// in flight. FAILED records DEGRADED with the agent's detail and answers
// with a fresh snapshot of the current version, never a retried delta
// (ADR-0015).
func (s *Server) handleAck(ctx context.Context, log *slog.Logger, sess *session, ack *innerwallv1.PolicyAck, now time.Time) error {
	switch ack.GetStatus() {
	case innerwallv1.AckStatus_ACK_STATUS_APPLIED:
		sess.mu.Lock()
		tip := sess.lastSent.GetVersion()
		sess.mu.Unlock()
		state := innerwallv1.SyncState_SYNC_STATE_PENDING
		if ack.GetVersion() >= tip {
			state = innerwallv1.SyncState_SYNC_STATE_SYNCED
		}
		if err := s.registry.RecordApplied(ctx, sess.id, ack.GetVersion(), state, now); err != nil {
			return status.Error(codes.Internal, "recording applied version")
		}
		log.Info("policy applied", "version", ack.GetVersion(), "state", state)
		return nil
	case innerwallv1.AckStatus_ACK_STATUS_FAILED:
		log.Error("policy apply failed on agent", "version", ack.GetVersion(), "detail", ack.GetErrorDetail())
		if err := s.registry.SetSyncState(ctx, sess.id, innerwallv1.SyncState_SYNC_STATE_DEGRADED, ack.GetErrorDetail(), now); err != nil {
			return status.Error(codes.Internal, "recording sync state")
		}
		sess.mu.Lock()
		stale := ack.GetVersion() <= sess.recovery
		sess.mu.Unlock()
		if stale {
			// A delta that was already in flight when the recovery
			// snapshot went out, or the recovery snapshot itself. Sending
			// the same snapshot again would loop; the next render sends
			// a new one.
			log.Info("failed ack predates the last recovery snapshot; not resending", "version", ack.GetVersion())
			return nil
		}
		return s.sendRecoverySnapshot(ctx, log, sess)
	case innerwallv1.AckStatus_ACK_STATUS_UNSPECIFIED:
		return status.Error(codes.InvalidArgument, "ack status must be APPLIED or FAILED")
	default:
		return status.Error(codes.InvalidArgument, "unknown ack status")
	}
}

// sendRecoverySnapshot answers a failed apply with the current persisted
// policy as a snapshot, resetting the stream's tip to it.
func (s *Server) sendRecoverySnapshot(ctx context.Context, log *slog.Logger, sess *session) error {
	policy, err := s.currentPolicy(ctx, sess.id)
	if err != nil {
		return status.Error(codes.Internal, "loading policy")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.lastSent = policy
	sess.recovery = policy.GetVersion()
	sess.enqueue(&innerwallv1.SyncResponse{Msg: &innerwallv1.SyncResponse_PolicyUpdate{PolicyUpdate: &innerwallv1.PolicyUpdate{Update: &innerwallv1.PolicyUpdate_Snapshot{Snapshot: &innerwallv1.PolicySnapshot{Policy: policy}}}}})
	log.Info("recovery snapshot sent", "version", policy.GetVersion())
	return nil
}

// currentPolicy returns the persisted policy of id, rendering first if
// none exists yet. Every enrolled workload is rendered at enrollment, so
// the render here is a fallback for a record that predates it.
func (s *Server) currentPolicy(ctx context.Context, id identity.WorkloadID) (*innerwallv1.WorkloadPolicy, error) {
	p, err := s.policies.GetWorkloadPolicy(ctx, id)
	if err != nil {
		return nil, err
	}
	if p != nil {
		return p, nil
	}
	if _, err := s.engine.Render(ctx); err != nil {
		return nil, err
	}
	p, err = s.policies.GetWorkloadPolicy(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("gateway: no policy for %s after rendering", id)
	}
	return p, nil
}

// pushLatest sends the persisted policy to sess as a delta from the tip of
// what the stream has already received, if it is newer. Runs whenever a
// render announces the workload changed and whenever the announcement
// channel (re)connects, so a lost announcement costs at most one
// reconciliation.
func (s *Server) pushLatest(sess *session) {
	log := s.log.With("workload_id", sess.id)
	policy, err := s.policies.GetWorkloadPolicy(sess.ctx, sess.id)
	if err != nil {
		if sess.ctx.Err() == nil {
			log.Error("loading policy for push", "error", err)
		}
		return
	}
	if policy == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.lastSent == nil || policy.GetVersion() <= sess.lastSent.GetVersion() {
		return
	}
	delta := rendered.Delta(sess.lastSent, policy)
	sess.lastSent = policy
	if !sess.enqueue(&innerwallv1.SyncResponse{Msg: &innerwallv1.SyncResponse_PolicyUpdate{PolicyUpdate: &innerwallv1.PolicyUpdate{Update: &innerwallv1.PolicyUpdate_Delta{Delta: delta}}}}) {
		return
	}
	if err := s.registry.SetSyncState(sess.ctx, sess.id, innerwallv1.SyncState_SYNC_STATE_PENDING, "", s.now()); err != nil && sess.ctx.Err() == nil {
		log.Error("recording sync state", "error", err)
	}
	log.Info("policy delta pushed", "from_version", delta.GetFromVersion(), "to_version", delta.GetToVersion(), "changes", len(delta.GetChanges()))
}

// Run listens for render announcements and routes each to the stream of
// the workload it names, until ctx ends. It must run for pushes to happen;
// without it, agents receive policy only at connect. Whenever the listener
// (re)connects, every live stream is reconciled against persisted state.
func (s *Server) Run(ctx context.Context) error {
	if s.events == nil {
		<-ctx.Done()
		return nil
	}
	return s.events.ListenPolicyChanges(ctx, s.log, func() {
		for _, sess := range s.sessions.all() {
			go s.pushLatest(sess)
		}
	}, func(c compiler.Announcement) {
		if sess := s.sessions.get(c.ID); sess != nil {
			go s.pushLatest(sess)
		}
	})
}
