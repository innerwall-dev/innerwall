package gateway_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestClockSkewRecordedOnTheWorkload runs clock-skew detection over a
// real ReportFlows stream into Postgres, with a one-minute flow window
// (two minutes' tolerance). A report stamped by a clock 72 minutes
// behind is stored as reported and marks the workload's skew current
// with its offset, which the operator read model returns. A window held
// in the buffer and delivered an hour late by an agent in step, stamped
// at send, is not skew, and clears the mark while keeping the last
// skewed offset and instant. A report without the stamp, from an agent
// that predates it, is judged by its window's end and only for a clock
// running ahead: an hour-late window is not skew, a window ending ten
// minutes ahead is.
func TestClockSkewRecordedOnTheWorkload(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "db", []enroll.Label{{Key: "role", Value: "db"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	db := enrollAgent(t, h, token, "db-1", "10.0.0.10/24")
	holder, err := credential.LoadHolder(*db.cred)
	if err != nil {
		t.Fatal(err)
	}
	client, closer, err := agentsync.DialGRPC(ctx, h.addr, holder)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closer.Close() }()
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}

	report := func(end time.Time, sent *time.Time) {
		t.Helper()
		stream, err := client.ReportFlows(ctx)
		if err != nil {
			t.Fatal(err)
		}
		req := &innerwallv1.ReportFlowsRequest{
			WindowStart: timestamppb.New(end.Add(-time.Minute)), WindowEnd: timestamppb.New(end),
			Records: []*innerwallv1.FlowRecord{{SrcAddress: "203.0.113.9", DstAddress: "10.0.0.10", DstPort: 5432, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ConnectionCount: 1}},
		}
		if sent != nil {
			req.SentAt = timestamppb.New(*sent)
		}
		if err := stream.Send(req); err != nil {
			t.Fatal(err)
		}
		if res, err := stream.CloseAndRecv(); err != nil || res.GetAcceptedRecords() != 1 {
			t.Fatalf("report = %v, %v; want one record accepted", res, err)
		}
	}
	near := func(got, want time.Duration) bool { d := got - want; return d > -5*time.Second && d < 5*time.Second }

	// A clock 72 minutes behind: stored as reported, skew current.
	before := time.Now()
	behind := before.Add(-72 * time.Minute)
	report(behind, &behind)
	rows, err := st.Flows().ListWindows(ctx, flowstore.WindowQuery{WorkloadID: db.id, Since: behind.Add(-time.Hour), Until: before.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].WindowEnd.Equal(behind.Truncate(time.Microsecond)) {
		t.Fatalf("the skewed window was not stored as reported: %+v", rows)
	}
	w, err := reads.GetWorkload(ctx, db.id)
	if err != nil {
		t.Fatal(err)
	}
	skew := w.Health.ClockSkew
	if skew == nil || !skew.Current || !near(skew.Offset, -72*time.Minute) || skew.ObservedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("skew after a behind report = %+v", skew)
	}
	observed := skew.ObservedAt

	// An hour-late buffered window from an agent in step: not skew; the
	// mark clears and the last skewed reading stays.
	late, now := time.Now().Add(-time.Hour), time.Now()
	report(late, &now)
	w, err = reads.GetWorkload(ctx, db.id)
	if err != nil {
		t.Fatal(err)
	}
	if skew := w.Health.ClockSkew; skew == nil || skew.Current || !near(skew.Offset, -72*time.Minute) || !skew.ObservedAt.Equal(observed) {
		t.Fatalf("skew after a late window from a clock in step = %+v", skew)
	}

	// Unstamped: an hour-late window is not skew; a window ending ten
	// minutes ahead is.
	report(time.Now().Add(-time.Hour), nil)
	w, err = reads.GetWorkload(ctx, db.id)
	if err != nil {
		t.Fatal(err)
	}
	if skew := w.Health.ClockSkew; skew == nil || skew.Current || !skew.ObservedAt.Equal(observed) {
		t.Fatalf("skew after an unstamped late window = %+v", skew)
	}
	report(time.Now().Add(10*time.Minute), nil)
	w, err = reads.GetWorkload(ctx, db.id)
	if err != nil {
		t.Fatal(err)
	}
	if skew := w.Health.ClockSkew; skew == nil || !skew.Current || !near(skew.Offset, 10*time.Minute) {
		t.Fatalf("skew after an unstamped window ending ahead = %+v", skew)
	}
}
