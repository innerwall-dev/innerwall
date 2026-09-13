package flowstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

const (
	allowed    = innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED
	wouldBlock = innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK
	tcp        = innerwallv1.Protocol_PROTOCOL_TCP
)

// TestRollupGroups checks every named grouping against the seeded fleet:
// the counts per group, the snapping of the covered range to window
// bounds, the passthrough of the peer resolution stored at ingest, the
// filters, the two orders, and truncation.
func TestRollupGroups(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)
	flows := s.Flows()
	day := flowstore.GroupQuery{Since: f.Now.Add(-24 * time.Hour), Until: f.Now}

	t.Run("rule", func(t *testing.T) {
		q := day
		q.GroupBy = flowstore.GroupByRule
		res, err := flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		// The unmatched group (six records per window of would-block,
		// blocked, and observed traffic) outweighs the one rule.
		if len(res.Groups) != 2 || res.GroupCount != 2 || res.Truncated() {
			t.Fatalf("groups = %+v", res.Groups)
		}
		if res.Groups[0].RuleID != "" || res.Groups[0].FlowCount != 12 || res.Groups[0].ConnectionCount != 2*(3+9+1+50+8+200) {
			t.Fatalf("unmatched group = %+v", res.Groups[0])
		}
		if res.Groups[1].RuleID != f.DBRuleID || res.Groups[1].FlowCount != 2 || res.Groups[1].ConnectionCount != 240 || res.Groups[1].ByteCount != 960_000 {
			t.Fatalf("rule group = %+v", res.Groups[1])
		}
		if !res.EffectiveFrom.Equal(f.Window1) || !res.EffectiveTo.Equal(f.Window2.Add(f.WindowLength)) {
			t.Fatalf("covered %v..%v, want %v..%v", res.EffectiveFrom, res.EffectiveTo, f.Window1, f.Window2.Add(f.WindowLength))
		}
		if res.FlowCount != 14 || res.ConnectionCount != 2*(120+3+9+1+50+8+200) {
			t.Fatalf("totals = %d records %d connections", res.FlowCount, res.ConnectionCount)
		}
		// Rule hit counters: scoped to a workload and to the allowed
		// verdict, the one rule is the only group.
		q.WorkloadIDs, q.Decision = []identity.WorkloadID{f.DB}, allowed
		res, err = flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Groups) != 1 || res.Groups[0].RuleID != f.DBRuleID || res.Groups[0].ConnectionCount != 240 || res.FlowCount != 2 {
			t.Fatalf("scoped rule groups = %+v", res.Groups)
		}
	})

	t.Run("rule,peer", func(t *testing.T) {
		q := day
		q.GroupBy, q.Decision = flowstore.GroupByRulePeer, allowed
		res, err := flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Groups) != 1 {
			t.Fatalf("groups = %+v", res.Groups)
		}
		g := res.Groups[0]
		if g.RuleID != f.DBRuleID || g.Peer.Kind != flowstore.PeerWorkload || g.Peer.Key != f.Web.String() {
			t.Fatalf("group = %+v", g)
		}
		// The label snapshot is the one ingestion stored, not a lookup.
		if g.Peer.Labels["role"] != "web" || g.Peer.Labels["env"] != "prod" {
			t.Fatalf("peer labels = %v", g.Peer.Labels)
		}
	})

	t.Run("src,dst would-block", func(t *testing.T) {
		q := day
		q.GroupBy, q.Decision = flowstore.GroupBySrcDst, wouldBlock
		res, err := flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Groups) != 3 || res.GroupCount != 3 {
			t.Fatalf("groups = %+v", res.Groups)
		}
		// Busiest first: the unknown address, the office group, then
		// web-1 on the port no rule admits.
		want := []struct {
			kind  flowstore.PeerKind
			key   string
			conns uint64
		}{
			{flowstore.PeerUnknown, "198.51.100.7", 18},
			{flowstore.PeerAddressGroup, f.Office.ID.String(), 6},
			{flowstore.PeerWorkload, f.Web.String(), 2},
		}
		for i, w := range want {
			g := res.Groups[i]
			if g.Peer.Kind != w.kind || g.Peer.Key != w.key || g.ConnectionCount != w.conns || g.WorkloadID != f.DB || g.FlowCount != 2 {
				t.Fatalf("group %d = %+v, want %+v", i, g, w)
			}
		}
		if res.ConnectionCount != 26 || res.FlowCount != 6 {
			t.Fatalf("totals = %+v", res)
		}
	})

	t.Run("dst,service", func(t *testing.T) {
		q := day
		q.GroupBy = flowstore.GroupByDstService
		res, err := flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Groups) != 4 {
			t.Fatalf("groups = %+v", res.Groups)
		}
		// cache-1 tcp/6379 (400), db-1 tcp/5432 (246), web-1 tcp/443
		// (116), db-1 tcp/22 (20).
		if g := res.Groups[0]; g.WorkloadID != f.Cache || g.DstPort != 6379 || g.Protocol != tcp || g.ConnectionCount != 400 {
			t.Fatalf("group 0 = %+v", g)
		}
		if g := res.Groups[1]; g.WorkloadID != f.DB || g.DstPort != 5432 || g.ConnectionCount != 246 || g.FlowCount != 4 {
			t.Fatalf("group 1 = %+v", g)
		}
		if g := res.Groups[3]; g.WorkloadID != f.DB || g.DstPort != 22 || g.ConnectionCount != 20 {
			t.Fatalf("group 3 = %+v", g)
		}
		// One service.
		q.Protocol, q.DstPort = tcp, 5432
		res, err = flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Groups) != 1 || res.Groups[0].DstPort != 5432 || res.GroupCount != 1 {
			t.Fatalf("service-filtered groups = %+v", res.Groups)
		}
	})

	t.Run("range snaps to windows", func(t *testing.T) {
		q := day
		q.GroupBy, q.Since = flowstore.GroupByRule, f.Now.Add(-90*time.Minute)
		res, err := flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if !res.EffectiveFrom.Equal(f.Window2) || !res.EffectiveTo.Equal(f.Window2.Add(f.WindowLength)) {
			t.Fatalf("covered %v..%v, want the second window only", res.EffectiveFrom, res.EffectiveTo)
		}
		if res.FlowCount != 7 {
			t.Fatalf("records = %d, want one window's 7", res.FlowCount)
		}
		q.Since, q.Until = f.Now.Add(-30*time.Minute), f.Now
		res, err = flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Groups) != 0 || !res.EffectiveFrom.IsZero() || res.GroupCount != 0 {
			t.Fatalf("empty range = %+v", res)
		}
	})

	t.Run("truncation and order", func(t *testing.T) {
		q := day
		q.GroupBy, q.Limit = flowstore.GroupBySrcDst, 2
		res, err := flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		full, err := flows.RollupGroups(ctx, flowstore.GroupQuery{GroupBy: flowstore.GroupBySrcDst, Since: q.Since, Until: q.Until})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Groups) != 2 || !res.Truncated() || res.GroupCount != full.GroupCount || full.Truncated() {
			t.Fatalf("truncated = %d of %d (truncated=%v)", len(res.Groups), res.GroupCount, res.Truncated())
		}
		if res.ConnectionCount != full.ConnectionCount || res.FlowCount != full.FlowCount || !res.EffectiveFrom.Equal(full.EffectiveFrom) {
			t.Fatal("a truncated rollup's totals and covered range must be the whole's")
		}
		if res.Groups[0].ConnectionCount < res.Groups[1].ConnectionCount || res.Groups[0].ConnectionCount != full.Groups[0].ConnectionCount {
			t.Fatalf("not the top groups: %+v", res.Groups)
		}
		q.Order, q.Limit = flowstore.OrderByRecency, 0
		recent, err := flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(recent.Groups); i++ {
			if recent.Groups[i].LastSeen.After(recent.Groups[i-1].LastSeen) {
				t.Fatalf("recency order broken at %d: %v after %v", i, recent.Groups[i].LastSeen, recent.Groups[i-1].LastSeen)
			}
		}
	})

	t.Run("filters", func(t *testing.T) {
		q := day
		q.GroupBy, q.Direction = flowstore.GroupByRule, innerwallv1.Direction_DIRECTION_OUTBOUND
		res, err := flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Groups) != 0 {
			t.Fatalf("outbound groups = %+v; every seeded flow is inbound", res.Groups)
		}
		q.Direction, q.WorkloadIDs = innerwallv1.Direction_DIRECTION_INBOUND, []identity.WorkloadID{f.Web, f.Cache}
		res, err = flows.RollupGroups(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if res.FlowCount != 6 || len(res.Groups) != 1 || res.Groups[0].RuleID != "" {
			t.Fatalf("scoped groups = %+v", res)
		}
	})

	if _, err := flows.RollupGroups(ctx, flowstore.GroupQuery{GroupBy: "peer,rule", Since: day.Since, Until: day.Until}); !errors.Is(err, flowstore.ErrUnknownGroupBy) {
		t.Fatalf("free-form grouping err = %v", err)
	}
}

// TestListWindowPage walks a workload's windows by cursor and checks that
// a window landing between pages neither repeats nor hides a row.
func TestListWindowPage(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)
	flows := s.Flows()
	base := flowstore.WindowPageQuery{WorkloadID: f.DB, Since: f.Now.Add(-24 * time.Hour), Until: f.Now, Limit: 3}

	all, err := flows.ListWindowPage(ctx, flowstore.WindowPageQuery{WorkloadID: f.DB, Since: base.Since, Until: base.Until, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 8 {
		t.Fatalf("db-1 has %d rows, want 8", len(all))
	}
	for i := 1; i < len(all); i++ {
		a, b := all[i-1], all[i]
		if b.WindowStart.After(a.WindowStart) || (b.WindowStart.Equal(a.WindowStart) && b.ID > a.ID) {
			t.Fatalf("rows not in (window_start, id) descending order at %d", i)
		}
	}

	page1, err := flows.ListWindowPage(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 3 {
		t.Fatalf("page 1 = %d rows", len(page1))
	}
	// A newer window lands between the pages: one more allowed
	// connection from web-1 on tcp/5432.
	extra := all[0].Record
	for i := range all {
		if all[i].DstPort == 5432 && all[i].Decision == allowed {
			extra = all[i].Record
			break
		}
	}
	newer := flowstore.Window{WorkloadID: f.DB, Start: f.Now.Add(-30 * time.Minute), End: f.Now.Add(-25 * time.Minute), Records: []flowstore.Record{extra}}
	if _, err := flows.WriteWindow(ctx, newer); err != nil {
		t.Fatal(err)
	}
	var walked []flowstore.WindowRow
	walked = append(walked, page1...)
	cursor := &flowstore.WindowCursor{WindowStart: page1[len(page1)-1].WindowStart, ID: page1[len(page1)-1].ID}
	for {
		q := base
		q.Before = cursor
		page, err := flows.ListWindowPage(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		walked = append(walked, page...)
		cursor = &flowstore.WindowCursor{WindowStart: page[len(page)-1].WindowStart, ID: page[len(page)-1].ID}
	}
	if len(walked) != len(all) {
		t.Fatalf("walked %d rows, want the %d that existed when the walk began", len(walked), len(all))
	}
	for i := range all {
		if walked[i].ID != all[i].ID {
			t.Fatalf("row %d: walked id %d, want %d", i, walked[i].ID, all[i].ID)
		}
	}

	// Filters: one peer, one service, one verdict.
	q := base
	q.PeerKey, q.Limit = f.Web.String(), 100
	rows, err := flows.ListWindowPage(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 { // 2 windows × (5432 allowed + 22 would-block) + the newer window
		t.Fatalf("peer-filtered rows = %d", len(rows))
	}
	q.PeerKey, q.Protocol, q.DstPort = "", tcp, 22
	rows, err = flows.ListWindowPage(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("service-filtered rows = %d", len(rows))
	}
	q.Protocol, q.DstPort, q.Decision = 0, 0, allowed
	rows, err = flows.ListWindowPage(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Decision != allowed || r.MatchedRuleID != f.DBRuleID {
			t.Fatalf("verdict-filtered row = %+v", r)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("verdict-filtered rows = %d", len(rows))
	}
}
