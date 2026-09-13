package ingest

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

func mustID(t *testing.T) identity.WorkloadID {
	t.Helper()
	id, err := identity.NewWorkloadID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestResolvePrecedence checks that a workload's current address beats an
// address group containing it, that the most specific group beats a wider
// one, that an address matching nothing resolves to itself, and that the
// label snapshot is a copy of the workload's labels at that moment.
func TestResolvePrecedence(t *testing.T) {
	web := registry.Workload{ID: mustID(t), Labels: []registry.Label{{Key: "role", Value: "web"}, {Key: "env", Value: "prod"}}, Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.20"), netip.MustParseAddr("fd00::20")}}
	wide := policy.AddressGroup{ID: uuid.MustParse("00000000-0000-0000-0000-00000000aaaa"), Name: "corp", CIDRs: []string{"10.0.0.0/8"}}
	narrow := policy.AddressGroup{ID: uuid.MustParse("00000000-0000-0000-0000-00000000bbbb"), Name: "office", CIDRs: []string{"10.0.0.0/24", "192.168.1.7"}}
	idx := BuildIndex([]registry.Workload{web}, []policy.AddressGroup{wide, narrow})

	// Workload beats every group, and the labels are a snapshot.
	p := idx.Resolve(netip.MustParseAddr("10.0.0.20"))
	if p.Kind != flowstore.PeerWorkload || p.Key != web.ID.String() || p.Labels["role"] != "web" || p.Labels["env"] != "prod" {
		t.Fatalf("workload address resolved to %+v", p)
	}
	p.Labels["role"] = "changed"
	if idx.Resolve(netip.MustParseAddr("10.0.0.20")).Labels["role"] != "web" {
		t.Fatal("label snapshot aliases the index")
	}
	if p := idx.Resolve(netip.MustParseAddr("::ffff:10.0.0.20")); p.Kind != flowstore.PeerWorkload {
		t.Fatalf("mapped v4 address resolved to %+v", p)
	}
	// Most specific group wins.
	if p := idx.Resolve(netip.MustParseAddr("10.0.0.99")); p.Kind != flowstore.PeerAddressGroup || p.Key != narrow.ID.String() {
		t.Fatalf("10.0.0.99 resolved to %+v", p)
	}
	if p := idx.Resolve(netip.MustParseAddr("10.9.9.9")); p.Kind != flowstore.PeerAddressGroup || p.Key != wide.ID.String() {
		t.Fatalf("10.9.9.9 resolved to %+v", p)
	}
	// A bare address in a group is a host route.
	if p := idx.Resolve(netip.MustParseAddr("192.168.1.7")); p.Key != narrow.ID.String() {
		t.Fatalf("192.168.1.7 resolved to %+v", p)
	}
	// Nothing matches: the address is the peer.
	if p := idx.Resolve(netip.MustParseAddr("192.168.1.8")); p.Kind != flowstore.PeerUnknown || p.Key != "192.168.1.8" || len(p.Labels) != 0 {
		t.Fatalf("unknown resolved to %+v", p)
	}
}

type fakeDirectory struct {
	workloads []registry.Workload
	groups    []policy.AddressGroup
}

func (f *fakeDirectory) ListWorkloads(context.Context) ([]registry.Workload, error) {
	return f.workloads, nil
}

func (f *fakeDirectory) ListAddressGroups(context.Context) ([]policy.AddressGroup, error) {
	return f.groups, nil
}

type fakeFlows struct {
	flowstore.FlowStore
	windows []flowstore.Window
}

func (f *fakeFlows) WriteWindow(_ context.Context, w flowstore.Window) (int, error) {
	f.windows = append(f.windows, w)
	return len(w.Records), nil
}

// TestIngestValidatesAndResolves checks that malformed records are skipped
// while the rest of the window lands resolved, that an inverted window is
// refused, and that an unregistered reporter is refused.
func TestIngestValidatesAndResolves(t *testing.T) {
	db := registry.Workload{ID: mustID(t), Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	web := registry.Workload{ID: mustID(t), Labels: []registry.Label{{Key: "role", Value: "web"}}, Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.20")}}
	flows := &fakeFlows{}
	svc := &Service{Directory: &fakeDirectory{workloads: []registry.Workload{db, web}}, Flows: flows}
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	good := &innerwallv1.FlowRecord{SrcAddress: "10.0.0.20", DstAddress: "10.0.0.10", DstPort: 5432, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ConnectionCount: 3, ByteCount: 300}
	bad := []*innerwallv1.FlowRecord{
		{SrcAddress: "not-an-address", DstAddress: "10.0.0.10", Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED},
		{SrcAddress: "10.0.0.20", DstAddress: "10.0.0.10", DstPort: 70000, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED},
		{SrcAddress: "10.0.0.20", DstAddress: "10.0.0.10", DstPort: 80, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED},
		{SrcAddress: "10.0.0.20", DstAddress: "10.0.0.10", DstPort: 80, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_OUTBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED},
		{SrcAddress: "10.0.0.20", DstAddress: "10.0.0.10", DstPort: 80, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND},
		{SrcAddress: "10.0.0.20", DstAddress: "10.0.0.10", DstPort: 80, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, FirstSeen: timestamppb.New(end), LastSeen: timestamppb.New(start)},
	}
	req := &innerwallv1.ReportFlowsRequest{WindowStart: timestamppb.New(start), WindowEnd: timestamppb.New(end), Records: append([]*innerwallv1.FlowRecord{good}, bad...)}
	res, err := svc.Ingest(context.Background(), db.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 1 || res.Rejected != len(bad) {
		t.Fatalf("result = %+v, want 1 accepted and %d rejected", res, len(bad))
	}
	w := flows.windows[0]
	if w.WorkloadID != db.ID || !w.Start.Equal(start) || !w.End.Equal(end) || len(w.Records) != 1 {
		t.Fatalf("window = %+v", w)
	}
	r := w.Records[0]
	if r.Peer.Kind != flowstore.PeerWorkload || r.Peer.Key != web.ID.String() || r.Peer.Labels["role"] != "web" {
		t.Fatalf("peer = %+v", r.Peer)
	}
	// Unset instants default to the window bounds.
	if !r.FirstSeen.Equal(start) || !r.LastSeen.Equal(end) || r.ConnectionCount != 3 || r.ByteCount != 300 || r.DstPort != 5432 {
		t.Fatalf("record = %+v", r)
	}

	// Inverted window.
	_, err = svc.Ingest(context.Background(), db.ID, &innerwallv1.ReportFlowsRequest{WindowStart: timestamppb.New(end), WindowEnd: timestamppb.New(start)})
	if err != ErrInvalidWindow { //nolint:errorlint // sentinel returned directly
		t.Fatalf("inverted window err = %v", err)
	}
	// Unknown reporter.
	_, err = svc.Ingest(context.Background(), mustID(t), req)
	if err != registry.ErrWorkloadUnknown { //nolint:errorlint // sentinel returned directly
		t.Fatalf("unknown reporter err = %v", err)
	}
}
