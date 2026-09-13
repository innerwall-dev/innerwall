package gateway_test

import (
	"context"
	"io"
	"net/netip"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// chanSource is a collect.Source fed by the test: the conntrack boundary,
// faked.
type chanSource struct {
	ch chan collect.Observation
}

func (s *chanSource) Run(ctx context.Context, emit func(collect.Observation)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case o := <-s.ch:
			emit(o)
		}
	}
}

// observation is an inbound connection to the db workload's address.
func observation(src string, port uint16, proto innerwallv1.Protocol, conns, bytes uint64) collect.Observation {
	return collect.Observation{At: time.Now(), Src: netip.MustParseAddr(src), Dst: netip.MustParseAddr("10.0.0.10"), DstPort: port, Protocol: proto, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, Connections: conns, Bytes: bytes}
}

// waitForFlows polls the store for a workload's windows until cond holds.
// Polling is a test device only; the system under test never polls.
func waitForFlows(t *testing.T, flows flowstore.FlowStore, id identity.WorkloadID, what string, cond func([]flowstore.WindowRow) bool) []flowstore.WindowRow {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		rows, err := flows.ListWindows(context.Background(), flowstore.WindowQuery{WorkloadID: id, Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if cond(rows) {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; rows = %+v", what, rows)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestFlowPipelineEndToEnd runs the flow path host to storage against
// Postgres and a real TLS listener: a collector fed at the conntrack
// boundary closes windows, the reporter ships them over ReportFlows with
// the workload credential in batches capped by SyncConfig, ingestion
// resolves each record's source against workloads (with a label
// snapshot), then address groups, then nothing, and the stored rows
// answer the queries the command line issues. Then: a label change is
// captured by later windows and never rewrites earlier ones; the rollup
// and totals shapes are right; retention deletes only aged windows and
// leaves totals alone; a heartbeat's renewal status is persisted; and a
// malformed window is refused.
func TestFlowPipelineEndToEnd(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	flows := st.Flows()

	dbToken, _, err := h.service.MintToken(ctx, "db", []enroll.Label{{Key: "role", Value: "db"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	webToken, _, err := h.service.MintToken(ctx, "web", []enroll.Label{{Key: "role", Value: "web"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	db := enrollAgent(t, h, dbToken, "db-1", "10.0.0.10/24")
	web := enrollAgent(t, h, webToken, "web-1", "10.0.0.20/24")
	authoring := &policy.Authoring{Store: st, Renderer: renderer{h.engine}}
	corp := &policy.AddressGroup{Name: "corp", CIDRs: []string{"192.168.0.0/16"}}
	if err := authoring.CreateAddressGroup(ctx, corp); err != nil {
		t.Fatal(err)
	}

	// The agent side, wired as the daemon wires it, with the conntrack
	// boundary faked.
	holder, err := credential.LoadHolder(*db.cred)
	if err != nil {
		t.Fatal(err)
	}
	source := &chanSource{ch: make(chan collect.Observation, 64)}
	buffer := collect.NewBuffer(0)
	collector := &collect.Collector{Source: source, Buffer: buffer}
	reporter := &collect.Reporter{Buffer: buffer, BackoffBase: 50 * time.Millisecond, BackoffCap: time.Second, Dial: func(ctx context.Context) (innerwallv1.AgentServiceClient, io.Closer, error) {
		return agentsync.DialGRPC(ctx, h.addr, holder)
	}}
	cfg := &innerwallv1.SyncConfig{FlowAggregationWindowSeconds: 1, FlowBatchMaxRecords: 2}
	collector.SetConfig(cfg)
	reporter.SetConfig(cfg)
	if reporter.BatchMax() != 2 || collector.Window() != time.Second {
		t.Fatalf("config not adopted: batch %d window %v", reporter.BatchMax(), collector.Window())
	}
	actx, acancel := context.WithCancel(ctx)
	defer acancel()
	go func() { _ = collector.Run(actx) }()
	go func() { _ = reporter.Run(actx) }()

	// Window 1: a workload peer (two source events for one connection
	// key, collapsing), an address-group peer, an unknown peer, ICMP.
	source.ch <- observation("10.0.0.20", 5432, innerwallv1.Protocol_PROTOCOL_TCP, 1, 0)
	source.ch <- observation("10.0.0.20", 5432, innerwallv1.Protocol_PROTOCOL_TCP, 1, 4096)
	source.ch <- observation("192.168.1.5", 22, innerwallv1.Protocol_PROTOCOL_TCP, 1, 100)
	source.ch <- observation("203.0.113.9", 53, innerwallv1.Protocol_PROTOCOL_UDP, 3, 300)
	source.ch <- observation("203.0.113.9", 0, innerwallv1.Protocol_PROTOCOL_ICMP, 2, 128)
	rows := waitForFlows(t, flows, db.id, "first window", func(rows []flowstore.WindowRow) bool { return len(rows) == 4 })
	byKey := map[string]flowstore.WindowRow{}
	for _, r := range rows {
		byKey[r.SrcAddress.String()+"/"+r.Protocol.String()] = r
	}
	r := byKey["10.0.0.20/PROTOCOL_TCP"]
	if r.Peer.Kind != flowstore.PeerWorkload || r.Peer.Key != web.id.String() || r.Peer.Labels["role"] != "web" || len(r.Peer.Labels) != 1 {
		t.Fatalf("workload peer = %+v", r.Peer)
	}
	if r.ConnectionCount != 2 || r.ByteCount != 4096 || r.DstPort != 5432 || r.Decision != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED || r.Direction != innerwallv1.Direction_DIRECTION_INBOUND || r.MatchedRuleID != "" {
		t.Fatalf("workload record = %+v", r.Record)
	}
	if !r.WindowEnd.After(r.WindowStart) || r.LastSeen.Before(r.FirstSeen) || r.LastSeen.After(r.WindowEnd) {
		t.Fatalf("window bounds %v..%v seen %v..%v", r.WindowStart, r.WindowEnd, r.FirstSeen, r.LastSeen)
	}
	if g := byKey["192.168.1.5/PROTOCOL_TCP"]; g.Peer.Kind != flowstore.PeerAddressGroup || g.Peer.Key != corp.ID.String() || len(g.Peer.Labels) != 0 {
		t.Fatalf("group peer = %+v", g.Peer)
	}
	if u := byKey["203.0.113.9/PROTOCOL_UDP"]; u.Peer.Kind != flowstore.PeerUnknown || u.Peer.Key != "203.0.113.9" || u.ConnectionCount != 3 {
		t.Fatalf("unknown peer = %+v", u)
	}
	if i := byKey["203.0.113.9/PROTOCOL_ICMP"]; i.DstPort != 0 || i.ByteCount != 128 {
		t.Fatalf("icmp = %+v", i)
	}
	if buffer.Dropped() != 0 {
		t.Fatalf("dropped = %d", buffer.Dropped())
	}

	// A label change on the peer: the next window carries the new
	// snapshot; the first window keeps the old one.
	if err := st.SetWorkloadLabels(ctx, web.id, []registry.Label{{Key: "role", Value: "web"}, {Key: "env", Value: "prod"}}); err != nil {
		t.Fatal(err)
	}
	source.ch <- observation("10.0.0.20", 5432, innerwallv1.Protocol_PROTOCOL_TCP, 5, 500)
	rows = waitForFlows(t, flows, db.id, "second window", func(rows []flowstore.WindowRow) bool { return len(rows) == 5 })
	var snapshots []map[string]string
	for _, r := range rows {
		if r.Peer.Kind == flowstore.PeerWorkload {
			snapshots = append(snapshots, r.Peer.Labels)
		}
	}
	if len(snapshots) != 2 || snapshots[0]["env"] != "prod" || snapshots[1]["env"] != "" {
		t.Fatalf("label snapshots (newest first) = %v", snapshots)
	}

	// Query shapes. A time bound excludes everything.
	past, err := flows.ListWindows(ctx, flowstore.WindowQuery{WorkloadID: db.id, Since: time.Now().Add(-2 * time.Hour), Until: time.Now().Add(-time.Hour)})
	if err != nil || len(past) != 0 {
		t.Fatalf("past windows = %v, %v", past, err)
	}
	only, err := flows.ListWindows(ctx, flowstore.WindowQuery{WorkloadID: db.id, Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Hour), Decision: innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK})
	if err != nil || len(only) != 0 {
		t.Fatalf("would-block windows = %v, %v", only, err)
	}
	// Rollup over the role=db scope, grouped by peer and service.
	rollup, err := flows.Rollup(ctx, flowstore.RollupQuery{WorkloadIDs: []identity.WorkloadID{db.id}, Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Hour), Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED})
	if err != nil {
		t.Fatal(err)
	}
	if len(rollup) != 4 {
		t.Fatalf("rollup rows = %+v", rollup)
	}
	top := rollup[0]
	if top.Peer.Kind != flowstore.PeerWorkload || top.Peer.Key != web.id.String() || top.DstPort != 5432 || top.ConnectionCount != 7 || top.ByteCount != 4596 || top.Workloads != 1 {
		t.Fatalf("top rollup row = %+v", top)
	}
	if !top.LastSeen.After(top.FirstSeen) {
		t.Fatalf("rollup seen %v..%v", top.FirstSeen, top.LastSeen)
	}
	// Totals since first seen: one row per key, folded across windows.
	totals, err := flows.ListTotals(ctx, db.id, innerwallv1.PolicyDecision_POLICY_DECISION_UNSPECIFIED)
	if err != nil {
		t.Fatal(err)
	}
	if len(totals) != 4 {
		t.Fatalf("totals = %+v", totals)
	}
	var webTotal *flowstore.Total
	for i := range totals {
		if totals[i].Peer.Kind == flowstore.PeerWorkload {
			webTotal = &totals[i]
		}
	}
	if webTotal == nil || webTotal.ConnectionCount != 7 || webTotal.ByteCount != 4596 || webTotal.WindowCount != 2 || webTotal.Peer.Labels["env"] != "prod" {
		t.Fatalf("web total = %+v", webTotal)
	}
	if !webTotal.LastSeen.After(webTotal.FirstSeen) {
		t.Fatalf("total seen %v..%v", webTotal.FirstSeen, webTotal.LastSeen)
	}

	// Retention: an aged window is deleted, recent ones and totals stay.
	old := time.Now().Add(-40 * 24 * time.Hour)
	if _, err := flows.WriteWindow(ctx, flowstore.Window{WorkloadID: db.id, Start: old, End: old.Add(time.Minute), Records: []flowstore.Record{{
		Peer: flowstore.Peer{Kind: flowstore.PeerUnknown, Key: "198.51.100.1"}, SrcAddress: netip.MustParseAddr("198.51.100.1"), DstAddress: netip.MustParseAddr("10.0.0.10"),
		DstPort: 80, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED,
		ConnectionCount: 1, FirstSeen: old, LastSeen: old,
	}}}); err != nil {
		t.Fatal(err)
	}
	before, _ := flows.CountWindows(ctx)
	retention := &flowstore.Retention{Store: flows, Horizon: 30 * 24 * time.Hour}
	deleted, ran, err := retention.RunOnce(ctx)
	if err != nil || !ran || deleted != 1 {
		t.Fatalf("retention = %d, %v, %v", deleted, ran, err)
	}
	after, _ := flows.CountWindows(ctx)
	if after != before-1 || after != 5 {
		t.Fatalf("windows before/after retention = %d/%d", before, after)
	}
	totals, _ = flows.ListTotals(ctx, db.id, innerwallv1.PolicyDecision_POLICY_DECISION_UNSPECIFIED)
	if len(totals) != 5 {
		t.Fatalf("totals after retention = %d, want 5 (never pruned)", len(totals))
	}
	if deleted, ran, err := retention.RunOnce(ctx); err != nil || !ran || deleted != 0 {
		t.Fatalf("second retention = %d, %v, %v", deleted, ran, err)
	}

	// The heartbeat's renewal status lands in the registry and clears.
	db.connect(0, facts("db-1", "10.0.0.10/24"))
	db.expectHelloAck()
	db.expectSnapshot()
	db.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Heartbeat{Heartbeat: &innerwallv1.Heartbeat{UptimeSeconds: 1, DroppedFlowRecords: 9, CredentialRenewalError: "renewal refused: boom"}}})
	waitFor(t, st, db.id, "renewal error recorded", func(w *registry.Workload) bool {
		return w.CredentialRenewalError == "renewal refused: boom" && w.DroppedFlowRecords == 9
	})
	db.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Heartbeat{Heartbeat: &innerwallv1.Heartbeat{UptimeSeconds: 2}}})
	waitFor(t, st, db.id, "renewal error cleared", func(w *registry.Workload) bool {
		return w.CredentialRenewalError == "" && w.DroppedFlowRecords == 0
	})
	db.disconnect()

	// A malformed window is refused with InvalidArgument; the stream
	// carries the identity, the payload never does.
	client, closer, err := agentsync.DialGRPC(ctx, h.addr, holder)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closer.Close() }()
	stream, err := client.ReportFlows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := stream.Send(&innerwallv1.ReportFlowsRequest{WindowStart: timestamppb.New(now), WindowEnd: timestamppb.New(now.Add(-time.Minute))}); err != nil {
		t.Fatal(err)
	}
	_, err = stream.CloseAndRecv()
	wantCode(t, err, codes.InvalidArgument)
	// And an empty, well-formed stream accepts nothing.
	stream, err = client.ReportFlows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	res, err := stream.CloseAndRecv()
	if err != nil || res.GetAcceptedRecords() != 0 {
		t.Fatalf("empty stream = %v, %v", res, err)
	}
}
