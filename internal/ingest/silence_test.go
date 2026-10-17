package ingest

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// TestSilenceAllowanceMatchesLiveness pins the derivation's allowance to
// the read model's: the silence that would have turned the console's
// telemetry-live judgement off is the one that, once over, becomes an
// evidence gap, and no other.
func TestSilenceAllowanceMatchesLiveness(t *testing.T) {
	if SilenceWindows != readmodel.TelemetryLiveWindows {
		t.Fatalf("ingest.SilenceWindows = %d, readmodel.TelemetryLiveWindows = %d; the two judgements must agree", SilenceWindows, readmodel.TelemetryLiveWindows)
	}
}

// silenceService is one workload reporting through a service with stateful
// fakes, as the derivation tests need it.
func silenceService(t *testing.T) (*Service, *fakeFlows, *fakeReports, registry.Workload) {
	t.Helper()
	db := registry.Workload{ID: mustID(t), Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	flows := &fakeFlows{}
	reports := &fakeReports{}
	svc := &Service{Directory: &fakeDirectory{workloads: []registry.Workload{db}}, Flows: flows, Reports: reports, FlowWindow: time.Minute}
	return svc, flows, reports, db
}

// report builds one quiet window [start, start+1m) and ingests it with the
// receipt clock set to at.
func report(t *testing.T, svc *Service, id registry.Workload, start, at time.Time) Result {
	t.Helper()
	svc.Now = func() time.Time { return at }
	res, err := svc.Ingest(context.Background(), id.ID, &innerwallv1.ReportFlowsRequest{
		WindowStart: timestamppb.New(start), WindowEnd: timestamppb.New(start.Add(time.Minute)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestSilenceDerivedAcrossAgentOutage is the issue's repro (#114): the
// agent process stops after a report, nothing is heard for longer than the
// allowance, and the restarted process's first report closes a derived
// agent_silent gap from the last receipt to the first window the new
// process opened.
func TestSilenceDerivedAcrossAgentOutage(t *testing.T) {
	svc, flows, _, db := silenceService(t)
	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if res := report(t, svc, db, t0.Add(-time.Minute), t0); res.Derived != 0 {
		t.Fatalf("first report ever derived %d gaps", res.Derived)
	}
	// The process is down from t0; it restarts at t0+10m and its first
	// window closes a minute later.
	restart := t0.Add(10 * time.Minute)
	res := report(t, svc, db, restart, restart.Add(time.Minute))
	if res.Derived != 1 || len(flows.derived) != 1 {
		t.Fatalf("derived = %d, stored %d", res.Derived, len(flows.derived))
	}
	g := flows.derived[0]
	if g.Kind != innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT ||
		g.Source != innerwallv1.EvidenceSource_EVIDENCE_SOURCE_UNSPECIFIED || g.Count != nil {
		t.Fatalf("gap = %+v", g)
	}
	if !g.From.Equal(t0) || !g.To.Equal(restart) {
		t.Fatalf("gap bounds [%v, %v), want [%v, %v): last receipt to the restarted process's window start", g.From, g.To, t0, restart)
	}
	// The next window follows on schedule: no further gap.
	if res := report(t, svc, db, restart.Add(time.Minute), restart.Add(2*time.Minute)); res.Derived != 0 {
		t.Fatalf("steady reporting after the outage derived %d gaps", res.Derived)
	}
}

// heartbeat records on reports what a heartbeat received at at said: the
// process that sent it had run for uptime.
func heartbeat(reports *fakeReports, at time.Time, uptime time.Duration) {
	reports.proc = &registry.AgentProcess{StartedAt: at.Add(-uptime), HeartbeatAt: at}
}

// TestQuietWorkloadDerivesNoGap is the repro of issue #144: a workload
// that receives almost no inbound traffic, its agent running and its sync
// stream heartbeating throughout. The agent ships only windows that hold
// something, so reports arrive minutes apart; each long stretch used to
// derive an agent_silent gap for evidence that was never lost. One process
// ran through every silence, so nothing is derived.
func TestQuietWorkloadDerivesNoGap(t *testing.T) {
	svc, flows, reports, db := silenceService(t)
	started := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	t0 := started.Add(2 * time.Hour)
	// An hour of windows; x held a record and was shipped, . was empty
	// and never left the host. Runs of up to five empty windows.
	const pattern = "xx..xxx...xxxx.xx.....xxxxx..xxx.xxxx...xxxxxx..xx.xxxxx.."
	for i := range pattern {
		start := t0.Add(time.Duration(i) * time.Minute)
		end := start.Add(time.Minute)
		// Heartbeats every 30s on the sync stream, the last one 20s
		// before the window closes.
		heartbeat(reports, end.Add(-20*time.Second), end.Add(-20*time.Second).Sub(started))
		if pattern[i] != 'x' {
			continue
		}
		if res := report(t, svc, db, start, end.Add(500*time.Millisecond)); res.Derived != 0 {
			t.Fatalf("window %d: a quiet, running agent derived %d gaps", i, res.Derived)
		}
	}
	if len(flows.derived) != 0 {
		t.Fatalf("derived %v, want none", flows.derived)
	}
}

// TestSilenceDerivedThroughStaleHeartbeat: the proof must cover the whole
// silence. A real outage still derives its gap with the same bounds when
// the latest heartbeat on record is the dead process's (the restarted
// one's first report beat its first heartbeat), when it is the restarted
// process's, and when the stream is down so no recent heartbeat exists.
func TestSilenceDerivedThroughStaleHeartbeat(t *testing.T) {
	started := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	t0 := started.Add(2 * time.Hour)
	restart := t0.Add(10 * time.Minute)
	for name, proc := range map[string]*registry.AgentProcess{
		// The old process's last heartbeat, 20s before it died at t0+25s.
		"old process": {StartedAt: started, HeartbeatAt: t0.Add(5 * time.Second)},
		// The restarted process, heartbeating 30s after it started.
		"new process": {StartedAt: restart, HeartbeatAt: restart.Add(30 * time.Second)},
		"none":        nil,
	} {
		t.Run(name, func(t *testing.T) {
			svc, flows, reports, db := silenceService(t)
			report(t, svc, db, t0.Add(-time.Minute), t0)
			reports.proc = proc
			if res := report(t, svc, db, restart, restart.Add(time.Minute)); res.Derived != 1 {
				t.Fatalf("outage derived %d gaps, want 1", res.Derived)
			}
			if g := flows.derived[0]; !g.From.Equal(t0) || !g.To.Equal(restart) {
				t.Fatalf("gap [%v, %v), want [%v, %v)", g.From, g.To, t0, restart)
			}
		})
	}
	// A running process whose sync stream has been down for longer than a
	// window proves nothing about the silence: its latest heartbeat is too
	// old, and the derivation stands.
	svc, flows, reports, db := silenceService(t)
	report(t, svc, db, t0.Add(-time.Minute), t0)
	heartbeat(reports, t0.Add(time.Minute), t0.Add(time.Minute).Sub(started))
	at := t0.Add(5 * time.Minute)
	if res := report(t, svc, db, at.Add(-time.Minute), at); res.Derived != 1 || !flows.derived[0].From.Equal(t0) {
		t.Fatalf("stream down: derived %d (%v), want the gap from %v", res.Derived, flows.derived, t0)
	}
}

// TestQuickRestartDerivesNoGap is the acceptance property: an ordinary
// agent restart on a healthy host, quick enough to lose no whole window,
// sprays no spurious gaps — the silence stays within the allowance.
func TestQuickRestartDerivesNoGap(t *testing.T) {
	svc, flows, _, db := silenceService(t)
	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	report(t, svc, db, t0.Add(-time.Minute), t0)
	// Stopped at t0, back at t0+20s; the first window after start closes
	// at t0+80s, exactly one window late and within two windows.
	if res := report(t, svc, db, t0.Add(20*time.Second), t0.Add(80*time.Second)); res.Derived != 0 || len(flows.derived) != 0 {
		t.Fatalf("quick restart derived %d gaps (%v)", res.Derived, flows.derived)
	}
}

// TestLateDeliveryDerivesNoGap: a telemetry connection outage is not an
// agent outage. The agent buffers closed windows and ships them when the
// connection returns, so the first report after the silence carries a
// window that reaches back over it — evidence arriving late, not evidence
// lost — and nothing is derived; a long outage's buffer losses are the
// agent's own buffer_overflow gaps, not this kind, so the one silence is
// never stored twice.
func TestLateDeliveryDerivesNoGap(t *testing.T) {
	svc, flows, _, db := silenceService(t)
	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	report(t, svc, db, t0.Add(-time.Minute), t0)
	// The connection is down for ten minutes; the buffered window that
	// closed at t0+1m arrives first, starting at t0, when the last receipt
	// was also t0.
	if res := report(t, svc, db, t0, t0.Add(10*time.Minute)); res.Derived != 0 || len(flows.derived) != 0 {
		t.Fatalf("late delivery derived %d gaps (%v)", res.Derived, flows.derived)
	}
}

// TestSilenceBoundsClampToReceipt: a report whose window starts at or past
// the receipt instant (a forward-skewed agent clock), and one refused
// before its window is read, both close the gap at the receipt instant,
// the last bound the control plane's own clock stands behind.
func TestSilenceBoundsClampToReceipt(t *testing.T) {
	svc, flows, _, db := silenceService(t)
	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	report(t, svc, db, t0.Add(-time.Minute), t0)
	received := t0.Add(10 * time.Minute)
	svc.Now = func() time.Time { return received }
	// Window claims to start an hour ahead of the receipt.
	res, err := svc.Ingest(context.Background(), db.ID, &innerwallv1.ReportFlowsRequest{
		WindowStart: timestamppb.New(received.Add(time.Hour)), WindowEnd: timestamppb.New(received.Add(61 * time.Minute)),
	})
	if err != nil || res.Derived != 1 {
		t.Fatalf("skewed window: derived = %d, %v", res.Derived, err)
	}
	if g := flows.derived[0]; !g.From.Equal(t0) || !g.To.Equal(received) {
		t.Fatalf("skewed window: gap [%v, %v), want [%v, %v)", g.From, g.To, t0, received)
	}

	// A report with no window bounds is refused, but it still ends the
	// silence it proves: the receipt advances first, so the gap it
	// reveals is derived and closed at the receipt.
	flows.derived = nil
	received2 := received.Add(10 * time.Minute)
	svc.Now = func() time.Time { return received2 }
	res, err = svc.Ingest(context.Background(), db.ID, &innerwallv1.ReportFlowsRequest{})
	if !errors.Is(err, ErrInvalidWindow) || res.Derived != 1 {
		t.Fatalf("bare report: derived = %d, err = %v", res.Derived, err)
	}
	if g := flows.derived[0]; !g.From.Equal(received) || !g.To.Equal(received2) {
		t.Fatalf("bare report: gap [%v, %v), want [%v, %v)", g.From, g.To, received, received2)
	}
}

// TestSilenceNeedsRecorderAndWindow: with no report recorder or no
// configured flow window there is no silence judgement at all, a stale
// report derives nothing, and a failed gap write is logged, not fatal —
// the window still lands.
func TestSilenceNeedsRecorderAndWindow(t *testing.T) {
	svc, flows, reports, db := silenceService(t)
	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	report(t, svc, db, t0.Add(-time.Minute), t0)

	// A stale report, handled out of order, says nothing new.
	if res := report(t, svc, db, t0.Add(-3*time.Minute), t0.Add(-2*time.Minute)); res.Derived != 0 {
		t.Fatalf("stale report derived %d gaps", res.Derived)
	}

	// No window length to judge by: nothing derived however long the gap.
	svc.FlowWindow = 0
	if res := report(t, svc, db, t0.Add(time.Hour), t0.Add(61*time.Minute)); res.Derived != 0 {
		t.Fatalf("windowless service derived %d gaps", res.Derived)
	}
	svc.FlowWindow = time.Minute

	// A failed write costs the report nothing.
	flows.gapsErr = errors.New("database away")
	res := report(t, svc, db, t0.Add(2*time.Hour), t0.Add(121*time.Minute))
	if res.Derived != 0 || len(flows.derived) != 0 {
		t.Fatalf("failed write: derived = %d", res.Derived)
	}
	flows.gapsErr = nil

	// A failed receipt record means no judgement either.
	reports.err = errors.New("database away")
	if res := report(t, svc, db, t0.Add(3*time.Hour), t0.Add(181*time.Minute)); res.Derived != 0 {
		t.Fatalf("failed receipt: derived = %d", res.Derived)
	}
}

// TestAgentCannotReportItsOwnSilence: the one server-derived kind is
// refused from the wire — an agent cannot observe its own absence.
func TestAgentCannotReportItsOwnSilence(t *testing.T) {
	svc, flows, _, db := silenceService(t)
	start := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return start.Add(time.Minute) }
	res, err := svc.Ingest(context.Background(), db.ID, &innerwallv1.ReportFlowsRequest{
		WindowStart: timestamppb.New(start), WindowEnd: timestamppb.New(start.Add(time.Minute)),
		Gaps: []*innerwallv1.EvidenceGap{{
			Kind: innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT,
			From: timestamppb.New(start.Add(-time.Hour)), To: timestamppb.New(start),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Gaps != 0 || res.GapsRejected != 1 {
		t.Fatalf("result = %+v, want the reported agent_silent gap rejected", res)
	}
	if len(flows.windows) != 1 || len(flows.windows[0].Gaps) != 0 {
		t.Fatalf("windows = %+v, want one with no gaps", flows.windows)
	}
}
