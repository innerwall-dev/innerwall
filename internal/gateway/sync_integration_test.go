package gateway_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// testAgent is a minimal sync client: it enrolls, opens the stream, sends
// Hello, and exposes typed receives with deadlines.
type testAgent struct {
	t      *testing.T
	id     identity.WorkloadID
	cred   *credential.Store
	stream innerwallv1.AgentService_SyncClient
	msgs   chan *innerwallv1.SyncResponse
	errs   chan error
	cancel context.CancelFunc
	h      *harness
}

func enrollAgent(t *testing.T, h *harness, token, hostname string, addrs ...string) *testAgent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	st := &credential.Store{Dir: dir}
	key, err := credential.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	res, err := credential.Enroll(ctx, key, credential.EnrollOptions{Server: h.addr, Token: token, BootstrapCA: h.bundle, Facts: facts(hostname, addrs...)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(key, res.CertificatePEM, res.BundlePEM); err != nil {
		t.Fatal(err)
	}
	return &testAgent{t: t, id: res.WorkloadID, cred: st, h: h}
}

func facts(hostname string, addrs ...string) *innerwallv1.HostFacts {
	return &innerwallv1.HostFacts{Hostname: hostname, Interfaces: []*innerwallv1.NetworkInterface{{Name: "eth0", Addresses: addrs}}}
}

// connect opens a stream and sends Hello with the given applied version.
func (a *testAgent) connect(applied uint64, f *innerwallv1.HostFacts) {
	a.t.Helper()
	cred, trust, err := a.cred.Load()
	if err != nil {
		a.t.Fatal(err)
	}
	_ = trust
	conn := a.h.dial(a.t, &cred)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	stream, err := innerwallv1.NewAgentServiceClient(conn).Sync(ctx)
	if err != nil {
		a.t.Fatal(err)
	}
	a.stream = stream
	// One reader per stream; every expectation reads from its channel.
	a.msgs = make(chan *innerwallv1.SyncResponse, 64)
	a.errs = make(chan error, 1)
	go func(msgs chan<- *innerwallv1.SyncResponse, errs chan<- error) {
		for {
			m, err := stream.Recv()
			if err != nil {
				errs <- err
				return
			}
			msgs <- m
		}
	}(a.msgs, a.errs)
	if err := stream.Send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Hello{Hello: &innerwallv1.Hello{
		Agent: &innerwallv1.AgentInfo{Version: "test"}, AppliedPolicyVersion: applied, Facts: f,
	}}}); err != nil {
		a.t.Fatal(err)
	}
}

func (a *testAgent) disconnect() {
	a.cancel()
}

func (a *testAgent) recv() *innerwallv1.SyncResponse {
	a.t.Helper()
	select {
	case m := <-a.msgs:
		return m
	case err := <-a.errs:
		a.t.Fatalf("recv: %v", err)
		return nil
	case <-time.After(15 * time.Second):
		a.t.Fatal("timed out waiting for a stream message")
		return nil
	}
}

func (a *testAgent) expectHelloAck() *innerwallv1.SyncConfig {
	a.t.Helper()
	m := a.recv()
	if m.GetHelloAck() == nil {
		a.t.Fatalf("expected HelloAck, got %v", m)
	}
	return m.GetHelloAck().GetConfig()
}

func (a *testAgent) expectSnapshot() *innerwallv1.WorkloadPolicy {
	a.t.Helper()
	m := a.recv()
	if m.GetPolicyUpdate().GetSnapshot() == nil {
		a.t.Fatalf("expected snapshot, got %v", m)
	}
	return m.GetPolicyUpdate().GetSnapshot().GetPolicy()
}

func (a *testAgent) expectDelta() *innerwallv1.PolicyDelta {
	a.t.Helper()
	m := a.recv()
	if m.GetPolicyUpdate().GetDelta() == nil {
		a.t.Fatalf("expected delta, got %v", m)
	}
	return m.GetPolicyUpdate().GetDelta()
}

func (a *testAgent) expectNothing(d time.Duration) {
	a.t.Helper()
	select {
	case m := <-a.msgs:
		a.t.Fatalf("unexpected message %v", m)
	case <-time.After(d):
	}
}

func (a *testAgent) ack(version uint64, st innerwallv1.AckStatus, detail string) {
	a.t.Helper()
	if err := a.stream.Send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_PolicyAck{PolicyAck: &innerwallv1.PolicyAck{Version: version, Status: st, ErrorDetail: detail}}}); err != nil {
		a.t.Fatal(err)
	}
}

func (a *testAgent) send(msg *innerwallv1.SyncRequest) {
	a.t.Helper()
	if err := a.stream.Send(msg); err != nil {
		a.t.Fatal(err)
	}
}

// waitFor polls the registry for a workload condition. Polling is a test
// device only; the system under test never polls.
func waitFor(t *testing.T, st *store.Store, id identity.WorkloadID, what string, cond func(*registry.Workload) bool) *registry.Workload {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		w, err := st.LookupWorkload(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if cond(w) {
			return w
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; workload = %+v", what, w)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSyncStreamEndToEnd runs the stream lifecycle against Postgres and a
// real TLS listener: two workloads enroll and connect, both receive
// snapshots, authoring a ruleset scoped to one with the other as peer
// delivers a delta carrying the peer's host route, versions and sync
// states land in the store, a reconnect yields a fresh snapshot at the
// current version, a FAILED ack produces a recovery snapshot with the
// DEGRADED-then-SYNCED transitions, a facts change on the peer re-renders,
// and a control-plane restart preserves every version.
func TestSyncStreamEndToEnd(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dbToken, _, err := h.service.MintToken(ctx, "db", []enroll.Label{{Key: "role", Value: "db"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	webToken, _, err := h.service.MintToken(ctx, "web", []enroll.Label{{Key: "role", Value: "web"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	db := enrollAgent(t, h, dbToken, "db-1", "10.0.0.10/24")
	web := enrollAgent(t, h, webToken, "web-1", "10.0.0.20/24", "fd00::20/64")

	// Enrollment rendered a snapshot for each: version 1, empty rules.
	for _, a := range []*testAgent{db, web} {
		p, err := st.GetWorkloadPolicy(ctx, a.id)
		if err != nil || p == nil || p.GetVersion() != 1 || len(p.GetInboundRules()) != 0 {
			t.Fatalf("policy after enrollment = %v err=%v", p, err)
		}
	}

	// Connect both; each gets HelloAck then a snapshot at version 1.
	db.connect(0, facts("db-1", "10.0.0.10/24"))
	web.connect(0, facts("web-1", "10.0.0.20/24", "fd00::20/64"))
	if cfg := db.expectHelloAck(); cfg.GetHeartbeatIntervalSeconds() == 0 {
		t.Fatalf("config = %v", cfg)
	}
	web.expectHelloAck()
	dbSnap := db.expectSnapshot()
	webSnap := web.expectSnapshot()
	if dbSnap.GetVersion() != 1 || webSnap.GetVersion() != 1 {
		t.Fatalf("snapshot versions %d/%d", dbSnap.GetVersion(), webSnap.GetVersion())
	}
	waitFor(t, st, db.id, "pending after snapshot", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_PENDING })
	db.ack(1, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	web.ack(1, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, db.id, "synced at 1", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.AppliedVersion == 1 && w.Agent.Version == "test" && w.LastSeenAt != nil
	})
	waitFor(t, st, web.id, "synced at 1", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.AppliedVersion == 1
	})

	// Author a ruleset scoped to db with web as the peer, through the
	// authoring service exactly as the command line does.
	authoring := &policy.Authoring{Store: st, Renderer: renderer{h.engine}}
	pg := &policy.Service{Name: "postgres", Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 5432, End: 5432}}}}}
	if err := authoring.CreateService(ctx, pg); err != nil {
		t.Fatal(err)
	}
	// A service alone changes no rendered policy: no push to anyone.
	db.expectNothing(300 * time.Millisecond)
	rs := &policy.Ruleset{Name: "web-to-db", Enabled: true, Scope: policy.Selector{"role": {"db"}}, Rules: []policy.Rule{{
		Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
		Peers:      []policy.Peer{{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web"}}}},
		ServiceIDs: []uuid.UUID{pg.ID},
	}}}
	if err := authoring.CreateRuleset(ctx, rs); err != nil {
		t.Fatal(err)
	}
	ruleID := rendered.RuleID(rs.Rules[0].ID.String(), innerwallv1.Protocol_PROTOCOL_TCP)

	// db receives a delta 1->2 upserting the rule with web's host routes.
	delta := db.expectDelta()
	if delta.GetFromVersion() != 1 || delta.GetToVersion() != 2 || len(delta.GetChanges()) != 1 {
		t.Fatalf("delta = %v", delta)
	}
	up := delta.GetChanges()[0].GetUpsertRule()
	if up == nil || up.GetRuleId() != ruleID || len(up.GetPeerCidrs()) != 2 || up.GetPeerCidrs()[0] != "10.0.0.20/32" || up.GetPeerCidrs()[1] != "fd00::20/128" || up.GetPorts()[0].GetStart() != 5432 {
		t.Fatalf("upsert = %v", up)
	}
	// web is not in scope: nothing pushed, version unchanged.
	web.expectNothing(300 * time.Millisecond)
	if p, _ := st.GetWorkloadPolicy(ctx, web.id); p.GetVersion() != 1 {
		t.Fatalf("web version = %d", p.GetVersion())
	}
	waitFor(t, st, db.id, "pending at 2", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_PENDING })
	applied, err := rendered.Apply(dbSnap, delta)
	if err != nil {
		t.Fatal(err)
	}
	db.ack(2, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, db.id, "synced at 2", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.AppliedVersion == 2
	})
	persisted, _ := st.GetWorkloadPolicy(ctx, db.id)
	if persisted.GetVersion() != 2 || !rendered.Equal(persisted, applied) {
		t.Fatalf("persisted %v != applied %v", persisted, applied)
	}

	// Disconnect and reconnect: a fresh snapshot at the current version,
	// regardless of the claimed applied version; OFFLINE in between.
	db.disconnect()
	waitFor(t, st, db.id, "offline", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE })
	db.connect(2, facts("db-1", "10.0.0.10/24"))
	db.expectHelloAck()
	if snap := db.expectSnapshot(); snap.GetVersion() != 2 || !rendered.Equal(snap, applied) {
		t.Fatalf("reconnect snapshot = %v", snap)
	}
	db.ack(2, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, db.id, "synced after reconnect", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED })

	// A facts change on the peer re-renders db: its rule's peers change.
	web.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Inventory{Inventory: &innerwallv1.InventoryReport{
		Facts:             facts("web-1", "10.0.0.99/24"),
		ListeningServices: []*innerwallv1.ListeningService{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Port: 443, ProcessName: "httpd"}},
	}}})
	delta = db.expectDelta()
	if delta.GetFromVersion() != 2 || delta.GetToVersion() != 3 || len(delta.GetChanges()) != 2 {
		t.Fatalf("facts delta = %v", delta)
	}
	if add := delta.GetChanges()[0].GetAddPeers(); add == nil || add.GetPeerCidrs()[0] != "10.0.0.99/32" {
		t.Fatalf("facts delta = %v", delta)
	}
	if rm := delta.GetChanges()[1].GetRemovePeers(); rm == nil || len(rm.GetPeerCidrs()) != 2 {
		t.Fatalf("facts delta = %v", delta)
	}
	if svcs, _ := st.ListListeningServices(ctx, web.id); len(svcs) != 1 || svcs[0].Port != 443 || svcs[0].ProcessName != "httpd" {
		t.Fatalf("listening services = %+v", svcs)
	}
	// An identical report changes nothing.
	web.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Inventory{Inventory: &innerwallv1.InventoryReport{Facts: facts("web-1", "10.0.0.99/24")}}})
	db.expectNothing(300 * time.Millisecond)

	// FAILED ack: DEGRADED, then a recovery snapshot at the current
	// version, then SYNCED once that is applied.
	db.ack(3, innerwallv1.AckStatus_ACK_STATUS_FAILED, "simulated apply failure")
	waitFor(t, st, db.id, "degraded", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_DEGRADED && w.SyncError == "simulated apply failure" && w.AppliedVersion == 2
	})
	if snap := db.expectSnapshot(); snap.GetVersion() != 3 {
		t.Fatalf("recovery snapshot = %v", snap)
	}
	db.ack(3, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, db.id, "synced after recovery", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.AppliedVersion == 3 && w.SyncError == ""
	})

	// Heartbeat lands in the record.
	db.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Heartbeat{Heartbeat: &innerwallv1.Heartbeat{UptimeSeconds: 5, DroppedFlowRecords: 9}}})
	waitFor(t, st, db.id, "heartbeat", func(w *registry.Workload) bool { return w.DroppedFlowRecords == 9 })

	// Mode change is a delta with set_mode only.
	if err := st.SetWorkloadMode(ctx, db.id, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED); err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.Render(ctx); err != nil {
		t.Fatal(err)
	}
	delta = db.expectDelta()
	if delta.GetToVersion() != 4 || len(delta.GetChanges()) != 1 || delta.GetChanges()[0].GetSetMode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
		t.Fatalf("mode delta = %v", delta)
	}
	db.ack(4, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, db.id, "synced at 4", func(w *registry.Workload) bool { return w.AppliedVersion == 4 })

	// Restart the control plane against the same database: versions are
	// exactly what they were, and a re-render moves nothing.
	db.disconnect()
	web.disconnect()
	h2 := newHarnessWith(t, st, h.authority)
	if rep, err := h2.engine.Render(ctx); err != nil || len(rep.Changed) != 0 {
		t.Fatalf("render after restart = %+v err=%v", rep, err)
	}
	for _, want := range []struct {
		id identity.WorkloadID
		v  uint64
	}{{db.id, 4}, {web.id, 1}} {
		p, err := st.GetWorkloadPolicy(ctx, want.id)
		if err != nil || p.GetVersion() != want.v {
			t.Fatalf("version after restart = %d, want %d (err %v)", p.GetVersion(), want.v, err)
		}
	}
	db.h = h2
	db.connect(0, nil)
	db.expectHelloAck()
	if snap := db.expectSnapshot(); snap.GetVersion() != 4 || snap.GetMode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
		t.Fatalf("snapshot after restart = %v", snap)
	}
	db.disconnect()
}

func TestSyncRequiresHelloFirst(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h")
	cred, _, _ := a.cred.Load()
	conn := h.dial(t, &cred)
	stream, err := innerwallv1.NewAgentServiceClient(conn).Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Heartbeat{Heartbeat: &innerwallv1.Heartbeat{}}}); err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	wantCode(t, err, codes.InvalidArgument)
}

func TestSecondStreamSupersedesFirst(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h")
	a.connect(0, nil)
	a.expectHelloAck()
	a.expectSnapshot()
	firstErrs := a.errs
	a.connect(0, nil)
	a.expectHelloAck()
	a.expectSnapshot()
	// The first stream ends with Aborted; the workload stays online.
	select {
	case err := <-firstErrs:
		wantCode(t, err, codes.Aborted)
	case <-time.After(10 * time.Second):
		t.Fatal("first stream was not ended")
	}
	a.ack(1, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, a.id, "synced on second stream", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED })
}

// renderer adapts the engine to the authoring service's interface, as the
// command line does.
type renderer struct{ e *compiler.Engine }

func (r renderer) Render(ctx context.Context) error {
	_, err := r.e.Render(ctx)
	return err
}
