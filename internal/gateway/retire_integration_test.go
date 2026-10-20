package gateway_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/authn"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// refusedMessage is the one refusal a retired identity receives at every
// door (ADR-0023). The tests compare against it exactly, so a refusal that
// starts naming a reason is a failing test.
const refusedMessage = "workload credential refused"

// wantOpaqueRefusal checks err carries the opaque refusal and nothing
// more; a client may wrap the status it received.
func wantOpaqueRefusal(t *testing.T, what string, err error) {
	t.Helper()
	var se interface{ GRPCStatus() *status.Status }
	if !errors.As(err, &se) {
		t.Fatalf("%s: err = %v, want a gRPC status", what, err)
	}
	if st := se.GRPCStatus(); st.Code() != codes.Unauthenticated || st.Message() != refusedMessage {
		t.Fatalf("%s: err = %v, want Unauthenticated %q and nothing else", what, err, refusedMessage)
	}
}

// operatorContext is a context carrying the one operator, as the command
// line and the surface's middleware place it.
func operatorContext(ctx context.Context) context.Context {
	return authn.NewContext(ctx, &authn.Principal{})
}

// streamEnded waits for a's stream to end and returns why.
func streamEnded(t *testing.T, a *testAgent) error {
	t.Helper()
	for {
		select {
		case <-a.msgs:
		case err := <-a.errs:
			return err
		case <-time.After(15 * time.Second):
			t.Fatal("the stream did not end")
			return nil
		}
	}
}

// fleetOver is the fleet domain over st, ending streams through streams
// when it is not nil, as serve wires it.
func fleetOver(st *store.Store, h *harness, streams fleet.Streams) *fleet.Service {
	return &fleet.Service{
		Store: st, Engine: h.engine, Reads: &readmodel.Reader{Store: st, Flows: st.Flows(), FlowWindow: time.Minute},
		Directives: st, Streams: streams, FlowWindow: time.Minute,
	}
}

// TestRetirementEndToEnd retires a workload with a live stream against
// Postgres and a real TLS listener (ADR-0023): the stream ends at once
// with the opaque refusal; renewal, a new stream, and a flow report are
// refused the same way; the workload's addresses leave the peer set of the
// workload whose rule named it, whose version advances by one and is
// pushed as a delta, while a workload whose rendered output does not change
// keeps its version, and the retired workload's persisted policy stays as
// it was. Every write that would change a retired workload is refused with
// the instant, no selector resolves to it, and the fleet reads it as
// retired, outside the attention counts.
func TestRetirementEndToEnd(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	mint := func(role string) string {
		tok, _, err := h.service.MintToken(ctx, role, []enroll.Label{{Key: "role", Value: role}}, 0)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	db := enrollAgent(t, h, mint("db"), "db-1", "10.0.0.10/24")
	web := enrollAgent(t, h, mint("web"), "web-1", "10.0.0.20/24")
	other := enrollAgent(t, h, mint("other"), "other-1", "10.0.0.30/24")

	// db admits web on 5432; other is in no rule's scope or peers.
	authoring := &policy.Authoring{Store: st, Renderer: renderer{h.engine}}
	rs := &policy.Ruleset{Name: "web-to-db", Enabled: true, Scope: policy.Selector{"role": {"db"}}, Rules: []policy.Rule{{
		Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
		Peers:   []policy.Peer{{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web"}}}},
		Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 5432, End: 5432}}}},
	}}}
	if err := authoring.CreateRuleset(ctx, rs); err != nil {
		t.Fatal(err)
	}
	ruleID := rendered.RuleID(rs.Rules[0].ID.String(), innerwallv1.Protocol_PROTOCOL_TCP)

	db.connect(0, facts("db-1", "10.0.0.10/24"))
	web.connect(0, facts("web-1", "10.0.0.20/24"))
	db.expectHelloAck()
	web.expectHelloAck()
	dbSnap := db.expectSnapshot()
	web.expectSnapshot()
	if r := dbSnap.GetInboundRules(); len(r) != 1 || r[0].GetRuleId() != ruleID || len(r[0].GetPeerCidrs()) != 1 || r[0].GetPeerCidrs()[0] != "10.0.0.20/32" {
		t.Fatalf("db snapshot before retirement = %v", dbSnap)
	}
	versions := func() map[identity.WorkloadID]uint64 {
		out := map[identity.WorkloadID]uint64{}
		for _, a := range []*testAgent{db, web, other} {
			p, err := st.GetWorkloadPolicy(ctx, a.id)
			if err != nil || p == nil {
				t.Fatalf("policy of %s = %v err = %v", a.id, p, err)
			}
			out[a.id] = p.GetVersion()
		}
		return out
	}
	before := versions()
	webPolicy, _ := st.GetWorkloadPolicy(ctx, web.id)

	svc := fleetOver(st, h, h.gw)
	if _, err := svc.Retire(ctx, web.id); !errors.Is(err, authn.ErrNoPrincipal) {
		t.Fatalf("retire without a principal: err = %v", err)
	}
	octx := operatorContext(ctx)
	res, err := svc.Retire(octx, web.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rendered.Changed) != 1 || res.Rendered.Changed[0].ID != db.id {
		t.Fatalf("render report = %+v, want db alone advanced", res.Rendered)
	}

	// The live stream ends at once, with nothing said about why.
	wantOpaqueRefusal(t, "the retired workload's live stream", streamEnded(t, web))

	// db is pushed a delta removing web's address; other and the
	// retired workload keep their versions.
	delta := db.expectDelta()
	if delta.GetFromVersion() != before[db.id] || delta.GetToVersion() != before[db.id]+1 {
		t.Fatalf("delta = %v, want %d to %d", delta, before[db.id], before[db.id]+1)
	}
	next, err := rendered.Apply(dbSnap, delta)
	if err != nil {
		t.Fatal(err)
	}
	if r := next.GetInboundRules(); len(r) != 1 || len(r[0].GetPeerCidrs()) != 0 {
		t.Fatalf("db policy after retirement = %v, want the rule with no peers", next)
	}
	after := versions()
	if after[db.id] != before[db.id]+1 || after[other.id] != before[other.id] || after[web.id] != before[web.id] {
		t.Fatalf("versions %v -> %v: want db alone advanced by one", before, after)
	}
	if p, _ := st.GetWorkloadPolicy(ctx, web.id); !rendered.Equal(p, webPolicy) {
		t.Fatalf("the retired workload's persisted policy changed: %v -> %v", webPolicy, p)
	}

	// Every door refuses the retired credential the same way.
	cred, trust, err := web.cred.Load()
	if err != nil {
		t.Fatal(err)
	}
	key, err := web.cred.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	_, err = credential.Renew(ctx, key, credential.RenewOptions{Server: h.addr, Credential: cred, Trust: trust})
	wantOpaqueRefusal(t, "renewal", err)
	wantOpaqueRefusal(t, "a new stream", refusedSync(ctx, t, h, &cred))
	wantOpaqueRefusal(t, "a flow report", refusedFlows(ctx, t, h, &cred))

	// The operator surface is precise: a second retirement, a label edit,
	// a mode change, and a directed reconnect each name the instant.
	wantRetired := func(what string, err error) {
		t.Helper()
		var re *registry.RetiredError
		if !errors.As(err, &re) || re.ID != web.id || !re.RetiredAt.Equal(res.RetiredAt) {
			t.Fatalf("%s: err = %v, want retired at %v", what, err, res.RetiredAt)
		}
	}
	_, err = svc.Retire(octx, web.id)
	wantRetired("a second retirement", err)
	wantRetired("a label edit", svc.SetLabels(ctx, web.id, []registry.Label{{Key: "role", Value: "web"}}, ""))
	_, err = svc.ChangeMode(ctx, fleet.ModeChangeRequest{WorkloadIDs: []identity.WorkloadID{web.id}, TargetMode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, ExpectedMatchCount: 1}, true)
	wantRetired("a mode change by id", err)
	_, err = svc.RequestReconnect(ctx, web.id)
	wantRetired("a directed reconnect", err)
	if _, err := svc.Retire(octx, identity.FromUUID(uuid.Must(uuid.NewV7()))); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("retiring an unknown workload: err = %v", err)
	}
	if p, err := svc.PreviewSelector(ctx, policy.Selector{"role": {"web"}}); err != nil || len(p.Matched) != 0 {
		t.Fatalf("preview of the retired workload's labels = %+v err = %v", p, err)
	}

	// The fleet reads it as retired: on its own read, outside the active
	// list and the attention counts, in the retired list.
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}
	w, err := reads.GetWorkload(ctx, web.id)
	if err != nil || w.Lifecycle != readmodel.LifecycleRetired || w.RetiredAt == nil || !w.RetiredAt.Equal(res.RetiredAt) {
		t.Fatalf("workload read = %+v err = %v", w, err)
	}
	counts, err := reads.CountWorkloads(ctx)
	if err != nil || counts.Retired != 1 || counts.Total != 2 {
		t.Fatalf("counts = %+v err = %v, want two active and one retired", counts, err)
	}
	var inBuckets int64
	for _, n := range counts.BySync {
		inBuckets += n
	}
	if inBuckets+counts.Removed != counts.Total {
		t.Fatalf("counts = %+v: the buckets do not sum to the active total", counts)
	}
	active, err := reads.ListWorkloads(ctx, readmodel.WorkloadsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, aw := range active.Workloads {
		if aw.ID == web.id {
			t.Fatal("the retired workload is in the active fleet list")
		}
	}
	retired, err := reads.ListWorkloads(ctx, readmodel.WorkloadsRequest{Lifecycle: readmodel.LifecycleRetired})
	if err != nil || len(retired.Workloads) != 1 || retired.Workloads[0].ID != web.id {
		t.Fatalf("retired list = %+v err = %v", retired, err)
	}
	db.disconnect()
}

// TestRetirementEndsAStreamHeldElsewhere retires a workload whose stream
// another replica holds. Through a replica whose listener runs, the end
// directive ends it; through one with no listener, so that every directive
// is lost, the stream's next heartbeat ends it, since the write a
// heartbeat makes refuses a retired workload (ADR-0023).
func TestRetirementEndsAStreamHeldElsewhere(t *testing.T) {
	st := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	writer := newHarness(t, st) // the replica the operator reaches
	holder := newHarnessWith(t, st, writer.authority)
	deaf := newHarnessWith(t, st, writer.authority, func(d *gateway.Deps) { d.Events = nil })
	token, _, err := writer.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	svc := fleetOver(st, writer, writer.gw)
	octx := operatorContext(ctx)

	// The directive reaches the replica holding the stream.
	a := enrollAgent(t, writer, token, "h-1", "10.0.0.10/24")
	a.h = holder
	a.connect(0, nil)
	a.expectHelloAck()
	a.expectSnapshot()
	waitFor(t, st, a.id, "pending", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_PENDING })
	if _, err := svc.Retire(octx, a.id); err != nil {
		t.Fatal(err)
	}
	wantOpaqueRefusal(t, "a stream held by another replica", streamEnded(t, a))

	// A replica that hears nothing still ends the stream at its next
	// heartbeat.
	b := enrollAgent(t, writer, token, "h-2", "10.0.0.11/24")
	b.h = deaf
	b.connect(0, nil)
	b.expectHelloAck()
	b.expectSnapshot()
	waitFor(t, st, b.id, "pending", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_PENDING })
	if _, err := svc.Retire(octx, b.id); err != nil {
		t.Fatal(err)
	}
	// Nothing ends it before then: the replica heard no directive.
	b.expectNothing(time.Second)
	b.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Heartbeat{Heartbeat: &innerwallv1.Heartbeat{UptimeSeconds: 60}}})
	wantOpaqueRefusal(t, "a stream whose end directive was lost, at its next heartbeat", streamEnded(t, b))
}

// TestReenrollingARetiredHostIsANewIdentity enrolls a host, retires it,
// and enrolls the same host again with the same token: enrollment assigns
// a fresh identity and looks up no earlier workload, so the host returns
// as a new, active workload with a stream of its own, and the retired one
// stays retired (ADR-0023).
func TestReenrollingARetiredHostIsANewIdentity(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", []enroll.Label{{Key: "role", Value: "web"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	old := enrollAgent(t, h, token, "web-1", "10.0.0.20/24")
	if _, err := fleetOver(st, h, h.gw).Retire(operatorContext(ctx), old.id); err != nil {
		t.Fatal(err)
	}
	again := enrollAgent(t, h, token, "web-1", "10.0.0.20/24")
	if again.id == old.id {
		t.Fatalf("re-enrollment of a retired host reused its identity %s", old.id)
	}
	if w, err := st.LookupWorkload(ctx, old.id); err != nil || !w.Retired() {
		t.Fatalf("old workload = %+v err = %v, want retired", w, err)
	}
	if w, err := st.LookupWorkload(ctx, again.id); err != nil || w.Retired() || w.Hostname != "web-1" {
		t.Fatalf("new workload = %+v err = %v, want active", w, err)
	}
	again.connect(0, nil)
	again.expectHelloAck()
	again.expectSnapshot()
	again.disconnect()
}
