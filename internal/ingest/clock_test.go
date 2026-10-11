package ingest

import (
	"bytes"
	"context"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// TestReadClock checks the skew rule on both paths. A stamped report is
// skewed beyond twice the window in either direction and not at it. An
// unstamped report, from an agent that predates sent_at, is skewed only
// when its window ends ahead of receipt beyond the tolerance: a window
// delivered an hour late from the agent's buffer is not skew, and the
// stamped path tells that same late window from a slow clock.
func TestReadClock(t *testing.T) {
	window := time.Minute
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	req := func(end time.Duration, sent *time.Duration) *innerwallv1.ReportFlowsRequest {
		r := &innerwallv1.ReportFlowsRequest{WindowStart: timestamppb.New(at.Add(end - window)), WindowEnd: timestamppb.New(at.Add(end))}
		if sent != nil {
			r.SentAt = timestamppb.New(at.Add(*sent))
		}
		return r
	}
	d := func(x time.Duration) *time.Duration { return &x }
	cases := []struct {
		name       string
		req        *innerwallv1.ReportFlowsRequest
		offset     time.Duration
		measuredBy string
		skewed     bool
	}{
		{"stamped, in step", req(-time.Second, d(-200*time.Millisecond)), -200 * time.Millisecond, MeasuredBySentAt, false},
		{"stamped, behind past the tolerance", req(-72*time.Minute, d(-72*time.Minute)), -72 * time.Minute, MeasuredBySentAt, true},
		{"stamped, ahead past the tolerance", req(10*time.Minute, d(10*time.Minute)), 10 * time.Minute, MeasuredBySentAt, true},
		{"stamped, behind at the tolerance", req(-2*time.Minute, d(-2*time.Minute)), -2 * time.Minute, MeasuredBySentAt, false},
		{"stamped, a buffered window delivered late by an agent in step", req(-time.Hour, d(-time.Second)), -time.Second, MeasuredBySentAt, false},
		{"unstamped, in step", req(-time.Second, nil), -time.Second, MeasuredByWindowEnd, false},
		{"unstamped, a buffered window delivered an hour late", req(-time.Hour, nil), -time.Hour, MeasuredByWindowEnd, false},
		{"unstamped, a window ending ahead past the tolerance", req(5*time.Minute, nil), 5 * time.Minute, MeasuredByWindowEnd, true},
		{"unstamped, a window ending ahead at the tolerance", req(2*time.Minute, nil), 2 * time.Minute, MeasuredByWindowEnd, false},
	}
	for _, c := range cases {
		r := ReadClock(c.req, at, window)
		if r.Offset != c.offset || r.MeasuredBy != c.measuredBy || r.Skewed != c.skewed || !r.ReceivedAt.Equal(at) {
			t.Errorf("%s: reading = %+v, want offset %v by %s skewed %v", c.name, r, c.offset, c.measuredBy, c.skewed)
		}
	}
}

type clockCall struct {
	id     identity.WorkloadID
	offset time.Duration
	skewed bool
	at     time.Time
}

type fakeClock struct{ calls []clockCall }

func (f *fakeClock) RecordClockReading(_ context.Context, id identity.WorkloadID, offset time.Duration, skewed bool, at time.Time) error {
	f.calls = append(f.calls, clockCall{id, offset, skewed, at})
	return nil
}

// TestIngestRecordsClockSkew checks the ingest path: a skewed report is
// stored like any other, its reading is recorded on the reporting
// workload, and a WARN line names the workload and the offset; a report
// within tolerance records a reading that clears the skew and logs
// nothing; and with no flow window configured the check is off.
func TestIngestRecordsClockSkew(t *testing.T) {
	db := registry.Workload{ID: mustID(t), Hostname: "db-1", Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	var logged bytes.Buffer
	flows, clock := &fakeFlows{}, &fakeClock{}
	svc := &Service{
		Directory: &fakeDirectory{workloads: []registry.Workload{db}}, Flows: flows, Clock: clock,
		FlowWindow: time.Minute, Now: func() time.Time { return now },
		Log: slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	behind := now.Add(-72 * time.Minute)
	record := &innerwallv1.FlowRecord{SrcAddress: "10.0.0.20", DstAddress: "10.0.0.10", DstPort: 5432, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ConnectionCount: 1}
	skewed := &innerwallv1.ReportFlowsRequest{
		WindowStart: timestamppb.New(behind.Add(-time.Minute)), WindowEnd: timestamppb.New(behind), SentAt: timestamppb.New(behind),
		Records: []*innerwallv1.FlowRecord{record},
	}
	res, err := svc.Ingest(context.Background(), db.ID, skewed)
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 1 || len(flows.windows) != 1 || !flows.windows[0].End.Equal(behind) {
		t.Fatalf("a skewed window was not stored as reported: result %+v, windows %+v", res, flows.windows)
	}
	if len(clock.calls) != 1 || clock.calls[0] != (clockCall{db.ID, -72 * time.Minute, true, now}) {
		t.Fatalf("recorded readings = %+v", clock.calls)
	}
	line := logged.String()
	for _, want := range []string{"level=WARN", "agent clock skewed", "workload_id=" + db.ID.String(), "hostname=db-1", "offset=-1h12m0s", "offset_seconds=-4320", "measured_by=sent_at"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %q lacks %q", line, want)
		}
	}

	logged.Reset()
	inStep := &innerwallv1.ReportFlowsRequest{WindowStart: timestamppb.New(now.Add(-time.Minute)), WindowEnd: timestamppb.New(now), SentAt: timestamppb.New(now.Add(time.Second))}
	if _, err := svc.Ingest(context.Background(), db.ID, inStep); err != nil {
		t.Fatal(err)
	}
	if len(clock.calls) != 2 || clock.calls[1] != (clockCall{db.ID, time.Second, false, now}) {
		t.Fatalf("recorded readings = %+v", clock.calls)
	}
	if logged.Len() != 0 {
		t.Fatalf("a reading within tolerance logged %q", logged.String())
	}

	svc.FlowWindow = 0
	if _, err := svc.Ingest(context.Background(), db.ID, skewed); err != nil {
		t.Fatal(err)
	}
	if len(clock.calls) != 2 || logged.Len() != 0 {
		t.Fatalf("with no flow window the clock was still checked: %+v, %q", clock.calls, logged.String())
	}
}

// TestIngestReadsTheClockBeforeTheBounds is the regression for a report
// refused for its bounds that recorded nothing about the clock that
// inverted them: a clock stepped back while a window was open ends the
// window before it starts. The report is still refused, and stores
// nothing, but its send instant is read first, so the skew is recorded
// on the workload and logged. A report with neither a send instant nor a
// window end has no reading to record.
func TestIngestReadsTheClockBeforeTheBounds(t *testing.T) {
	db := registry.Workload{ID: mustID(t), Hostname: "db-1", Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	var logged bytes.Buffer
	flows, clock := &fakeFlows{}, &fakeClock{}
	svc := &Service{
		Directory: &fakeDirectory{workloads: []registry.Workload{db}}, Flows: flows, Clock: clock,
		FlowWindow: time.Minute, Now: func() time.Time { return now },
		Log: slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	// Opened in step; the clock fell back 30 minutes before it closed.
	stepped := now.Add(-30 * time.Minute)
	inverted := &innerwallv1.ReportFlowsRequest{
		WindowStart: timestamppb.New(now.Add(-time.Minute)), WindowEnd: timestamppb.New(stepped), SentAt: timestamppb.New(stepped),
		Records: []*innerwallv1.FlowRecord{{SrcAddress: "10.0.0.20", DstAddress: "10.0.0.10", DstPort: 5432, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ConnectionCount: 1}},
	}
	if _, err := svc.Ingest(context.Background(), db.ID, inverted); err != ErrInvalidWindow { //nolint:errorlint // sentinel returned directly
		t.Fatalf("inverted window: err = %v, want ErrInvalidWindow", err)
	}
	if len(flows.windows) != 0 {
		t.Fatalf("a refused window was stored: %+v", flows.windows)
	}
	if len(clock.calls) != 1 || clock.calls[0] != (clockCall{db.ID, -30 * time.Minute, true, now}) {
		t.Fatalf("recorded readings = %+v, want the refused report's skew", clock.calls)
	}
	for _, want := range []string{"agent clock skewed", "workload_id=" + db.ID.String(), "offset=-30m0s", "measured_by=sent_at"} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("log %q lacks %q", logged.String(), want)
		}
	}

	logged.Reset()
	if _, err := svc.Ingest(context.Background(), db.ID, &innerwallv1.ReportFlowsRequest{}); err != ErrInvalidWindow { //nolint:errorlint // sentinel returned directly
		t.Fatalf("bare report: err = %v, want ErrInvalidWindow", err)
	}
	if len(clock.calls) != 1 || logged.Len() != 0 {
		t.Fatalf("a report carrying no clock recorded a reading: %+v, %q", clock.calls, logged.String())
	}
}
