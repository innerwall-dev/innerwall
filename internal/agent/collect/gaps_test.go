package collect

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// TestGapsOpenCloseAndMerge checks the recorder: an interval opened at a
// failure is recorded when the source subscribes again; opening again
// while one is open keeps the earlier start, because nothing was observed
// since; another kind takes over at its own start; a closed interval that
// touches the newest of its kind and source widens it and sums the
// counts; one that does not is recorded on its own.
func TestGapsOpenCloseAndMerge(t *testing.T) {
	g := &Gaps{}
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

	g.Close(GapConntrack, at(0)) // nothing open: nothing recorded
	g.Open(GapConntrack, GapSourceRestart, at(1))
	g.Open(GapConntrack, GapSourceRestart, at(3))
	g.Close(GapConntrack, at(5))
	g.Open(GapNflog, GapSourceRestart, at(6))
	g.Open(GapNflog, GapSourceOverrun, at(8))
	g.Close(GapNflog, at(9))
	g.Record(Gap{Kind: GapDumpTruncated, Source: GapConntrack, From: at(10), To: at(11), Count: 4, HasCount: true})
	g.Record(Gap{Kind: GapDumpTruncated, Source: GapConntrack, From: at(10), To: at(14), HasCount: true})
	g.Record(Gap{Kind: GapDumpTruncated, Source: GapConntrack, From: at(20), To: at(21), Count: 1, HasCount: true})

	want := []Gap{
		{Kind: GapSourceRestart, Source: GapConntrack, From: at(1), To: at(5)},
		{Kind: GapSourceRestart, Source: GapNflog, From: at(6), To: at(8)},
		{Kind: GapSourceOverrun, Source: GapNflog, From: at(8), To: at(9)},
		{Kind: GapDumpTruncated, Source: GapConntrack, From: at(10), To: at(14), Count: 4, HasCount: true},
		{Kind: GapDumpTruncated, Source: GapConntrack, From: at(20), To: at(21), Count: 1, HasCount: true},
	}
	got := g.Pending()
	if len(got) != len(want) {
		t.Fatalf("gaps = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("gap %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// A merge with a gap of unknown count leaves the count unknown.
	g.Record(Gap{Kind: GapDumpTruncated, Source: GapConntrack, From: at(21), To: at(22)})
	if last := g.Pending()[4]; last.HasCount || last.Count != 0 || !last.To.Equal(at(22)) {
		t.Fatalf("merged with unknown count = %+v", last)
	}

	// A nil recorder records nothing and does not panic.
	var none *Gaps
	none.Open(GapConntrack, GapSourceRestart, at(0))
	none.Close(GapConntrack, at(1))
	none.Record(Gap{})
	none.Overrun()
	if none.Overruns() != 0 || none.Pending() != nil {
		t.Fatal("nil recorder recorded")
	}
}

// TestGapsStayBoundedByMerging checks that the recorder never holds more
// than its capacity and never forgets that a loss happened: past the
// bound, the two oldest gaps sharing a kind and source become one wider
// gap, so precision goes first where the evidence is oldest.
func TestGapsStayBoundedByMerging(t *testing.T) {
	g := &Gaps{Capacity: 3}
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	for i := range 5 {
		g.Record(Gap{Kind: GapSourceOverrun, Source: GapNflog, From: at(10 * i), To: at(10*i + 1)})
	}
	got := g.Pending()
	if len(got) != 3 {
		t.Fatalf("held %d gaps, want 3: %+v", len(got), got)
	}
	if !got[0].From.Equal(at(0)) || !got[0].To.Equal(at(21)) || !got[1].From.Equal(at(30)) || !got[2].From.Equal(at(40)) {
		t.Fatalf("gaps = %+v, want the oldest three merged and the newest two kept", got)
	}

	// A gap of another kind makes room the same way.
	g.Record(Gap{Kind: GapDumpTruncated, Source: GapConntrack, From: at(50), To: at(51), Count: 2, HasCount: true})
	got = g.Pending()
	if len(got) != 3 || got[2].Kind != GapDumpTruncated || !got[0].To.Equal(at(31)) || !got[1].From.Equal(at(40)) {
		t.Fatalf("after a new kind = %+v", got)
	}
}

// TestGapsTakeAndRestore checks shipping: Take empties the recorder;
// Restore puts undelivered gaps back ahead of newer ones, unmerged even
// where they touch, so a repeated delivery repeats them exactly; and the
// wire form carries a count only when one is known.
func TestGapsTakeAndRestore(t *testing.T) {
	g := &Gaps{}
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	first := Gap{Kind: GapSourceOverrun, Source: GapConntrack, From: at(0), To: at(1)}
	g.Record(first)
	taken := g.Take()
	if len(taken) != 1 || g.Held() != 0 {
		t.Fatalf("take = %+v, held after = %d", taken, g.Held())
	}
	g.Record(Gap{Kind: GapSourceOverrun, Source: GapConntrack, From: at(1), To: at(2)})
	g.Restore(taken)
	got := g.Pending()
	if len(got) != 2 || got[0] != first || !got[1].From.Equal(at(1)) {
		t.Fatalf("after restore = %+v", got)
	}

	w := first.Wire()
	if w.GetKind() != innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_OVERRUN || w.GetSource() != innerwallv1.EvidenceSource_EVIDENCE_SOURCE_CONNTRACK || w.Count != nil || !w.GetFrom().AsTime().Equal(at(0)) || !w.GetTo().AsTime().Equal(at(1)) {
		t.Fatalf("wire = %v", w)
	}
	w = Gap{Kind: GapBufferOverflow, Source: GapNoSource, From: at(0), To: at(60), Count: 0, HasCount: true}.Wire()
	if w.GetKind() != innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_BUFFER_OVERFLOW || w.GetSource() != innerwallv1.EvidenceSource_EVIDENCE_SOURCE_UNSPECIFIED || w.Count == nil || w.GetCount() != 0 {
		t.Fatalf("wire with a known zero count = %v", w)
	}
}

// TestNextAttemptResetsAfterHealthyRun checks the backoff's attempt
// count: it climbs while the source keeps failing quickly, and starts
// over after a run that stayed up longer than the cap.
func TestNextAttemptResetsAfterHealthyRun(t *testing.T) {
	const limit = time.Minute
	attempt := 0
	for range 4 {
		attempt = nextAttempt(attempt, time.Second, limit)
	}
	if attempt != 4 {
		t.Fatalf("attempt after quick failures = %d, want 4", attempt)
	}
	if attempt = nextAttempt(attempt, limit+time.Second, limit); attempt != 1 {
		t.Fatalf("attempt after a healthy run = %d, want 1", attempt)
	}
	if attempt = nextAttempt(attempt, limit, limit); attempt != 2 {
		t.Fatalf("attempt after a run exactly at the cap = %d, want 2", attempt)
	}
}

// failingSource fails every run, after advancing a fake clock by its next
// duration, and records the evidence gaps of a named source.
type failingSource struct {
	clock *fakeClock
	runs  []time.Duration
	gaps  *Gaps
	calls int
}

func (s *failingSource) Run(ctx context.Context, _ func(Observation)) error {
	s.clock.mu.Lock()
	n := s.calls
	s.clock.mu.Unlock()
	if n >= len(s.runs) {
		<-ctx.Done()
		return nil
	}
	s.clock.advance(s.runs[n])
	s.clock.mu.Lock()
	s.calls++
	s.clock.mu.Unlock()
	return errors.New("source failed")
}

func (s *failingSource) EvidenceGaps() (*Gaps, GapSource) { return s.gaps, GapConntrack }

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// TestRestarterBacksOffResetsAndOpensGaps drives the restart loop on a
// fake clock: the wait's ceiling doubles while the source fails quickly,
// falls back to the base after a healthy run, and every failure opens a
// restart gap for the source at the failure instant, which stays open
// across further failures until the source subscribes again.
func TestRestarterBacksOffResetsAndOpensGaps(t *testing.T) {
	clock := &fakeClock{now: t0}
	gaps := &Gaps{}
	const base, limit = time.Second, time.Minute
	src := &failingSource{clock: clock, gaps: gaps, runs: []time.Duration{
		time.Second, time.Second, time.Second, 2 * time.Minute, time.Second,
	}}
	var waits []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := restarter{
		log: slog.New(slog.DiscardHandler), base: base, limit: limit,
		rng: rand.New(rand.NewPCG(1, 2)), //nolint:gosec // test
		now: clock.Now,
		after: func(d time.Duration) <-chan time.Time {
			clock.mu.Lock()
			waits = append(waits, d)
			clock.mu.Unlock()
			ch := make(chan time.Time, 1)
			ch <- clock.Now()
			return ch
		},
	}
	done := make(chan struct{})
	go func() { r.run(ctx, src, func(Observation) {}); close(done) }()
	deadline := time.After(5 * time.Second)
	for {
		clock.mu.Lock()
		n, w := src.calls, len(waits)
		clock.mu.Unlock()
		if n == len(src.runs) && w == len(src.runs) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("restarter made %d runs and %d waits", n, w)
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done

	// Ceilings by attempt: 2s, 4s, 8s, then 2s after the healthy run,
	// then 4s. Full jitter keeps every wait below its ceiling.
	ceilings := []time.Duration{2 * base, 4 * base, 8 * base, 2 * base, 4 * base}
	for i, w := range waits {
		if w < 0 || w >= ceilings[i] {
			t.Fatalf("wait %d = %v, want below %v (waits %v)", i, w, ceilings[i], waits)
		}
	}

	// One gap is open from the first failure; the source never
	// subscribed, so closing it now records the whole outage.
	gaps.Close(GapConntrack, clock.Now())
	got := gaps.Pending()
	if len(got) != 1 || got[0].Kind != GapSourceRestart || !got[0].From.Equal(t0.Add(time.Second)) || !got[0].To.Equal(clock.Now()) {
		t.Fatalf("gaps = %+v", got)
	}
}

// TestBufferOverflowIsAGap checks the one loss model: every record the
// buffer drops is also a buffer_overflow gap over the dropped window's
// bounds, with the count dropped, whether a whole window was evicted or
// an oversized one was cut. Carry queues an empty window only when
// nothing is queued, so gaps always have a window to ride.
func TestBufferOverflowIsAGap(t *testing.T) {
	gaps := &Gaps{}
	b := NewBuffer(10)
	b.Gaps = gaps
	b.Push(window(t0, 6))
	b.Push(window(t0.Add(time.Minute), 6))
	got := gaps.Pending()
	if len(got) != 1 || got[0].Kind != GapBufferOverflow || got[0].Source != GapNoSource || !got[0].HasCount || got[0].Count != 6 || !got[0].From.Equal(t0) || !got[0].To.Equal(t0.Add(time.Minute)) {
		t.Fatalf("eviction gap = %+v", got)
	}
	b.Pop(context.Background())
	b.Push(window(t0.Add(2*time.Minute), 13))
	got = gaps.Pending()
	if len(got) != 2 || got[1].Count != 3 || !got[1].From.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("cut gap = %+v", got)
	}
	if b.Dropped() != 9 {
		t.Fatalf("dropped = %d, want 9: the heartbeat counter stays", b.Dropped())
	}

	b.Carry(window(t0, 0))
	if b.Len() != 1 {
		t.Fatalf("carry beside a queued window: len = %d, want 1", b.Len())
	}
	b.Pop(context.Background())
	b.Carry(window(t0, 0))
	if w, ok := b.Pop(context.Background()); !ok || w.Len() != 0 {
		t.Fatal("carry into an empty buffer queued nothing")
	}
}

// fakeFlows is an AgentServiceClient whose ReportFlows records what is
// sent and fails the stream when told to.
type fakeFlows struct {
	innerwallv1.AgentServiceClient
	mu   sync.Mutex
	sent []*innerwallv1.ReportFlowsRequest
	fail bool
}

type fakeFlowStream struct {
	grpc.ClientStream
	f *fakeFlows
}

func (f *fakeFlows) ReportFlows(context.Context, ...grpc.CallOption) (innerwallv1.AgentService_ReportFlowsClient, error) {
	return &fakeFlowStream{f: f}, nil
}

func (s *fakeFlowStream) Send(req *innerwallv1.ReportFlowsRequest) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.sent = append(s.f.sent, req)
	return nil
}

func (s *fakeFlowStream) CloseAndRecv() (*innerwallv1.ReportFlowsResponse, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if s.f.fail {
		return nil, errors.New("stream failed")
	}
	return &innerwallv1.ReportFlowsResponse{}, nil
}

// TestReporterShipsGapsWithTheWindow checks the reporter: the gaps held
// when a window is sent ride its first request, a failed stream puts them
// back to go again with the retry, and a delivered window leaves none
// held.
func TestReporterShipsGapsWithTheWindow(t *testing.T) {
	gaps := &Gaps{}
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	gaps.Record(Gap{Kind: GapSourceRestart, Source: GapNflog, From: at(0), To: at(5)})
	buf := NewBuffer(0)
	buf.Push(window(t0, 5))
	flows := &fakeFlows{fail: true}
	r := &Reporter{
		Buffer: buf, Gaps: gaps, BackoffBase: time.Millisecond, BackoffCap: time.Millisecond,
		Dial: func(context.Context) (innerwallv1.AgentServiceClient, io.Closer, error) {
			return flows, io.NopCloser(nil), nil
		},
		Log: slog.New(slog.DiscardHandler),
	}
	r.SetConfig(&innerwallv1.SyncConfig{FlowBatchMaxRecords: 2})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		flows.mu.Lock()
		n := len(flows.sent)
		flows.mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first attempt was not sent")
		}
		time.Sleep(time.Millisecond)
	}
	flows.mu.Lock()
	first := flows.sent[:3]
	flows.fail = false
	flows.mu.Unlock()
	if len(first[0].GetGaps()) != 1 || len(first[1].GetGaps()) != 0 || first[0].GetGaps()[0].GetKind() != innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_RESTART {
		t.Fatalf("gaps on the first attempt = %v / %v", first[0].GetGaps(), first[1].GetGaps())
	}

	deadline = time.Now().Add(5 * time.Second)
	for {
		flows.mu.Lock()
		n := len(flows.sent)
		flows.mu.Unlock()
		if n >= 6 && gaps.Held() == 0 && buf.Len() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not delivered: %d requests sent, %d gaps held, %d windows queued", n, gaps.Held(), buf.Len())
		}
		time.Sleep(time.Millisecond)
	}
	flows.mu.Lock()
	retry := flows.sent[3]
	flows.mu.Unlock()
	if len(retry.GetGaps()) != 1 || !retry.GetGaps()[0].GetTo().AsTime().Equal(at(5)) {
		t.Fatalf("gaps on the retry = %v", retry.GetGaps())
	}
	cancel()
	<-done
}
