package collect

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"testing"
	"time"
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
// bound, a gap joins the newest of its kind and source, widening it, or
// two older gaps of one kind and source are merged to make room.
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
	if !got[2].From.Equal(at(20)) || !got[2].To.Equal(at(41)) {
		t.Fatalf("newest gap = %+v, want it widened to cover the overflow", got[2])
	}

	// A kind the recorder holds none of makes room by merging two of
	// another kind.
	g.Record(Gap{Kind: GapDumpTruncated, Source: GapConntrack, From: at(50), To: at(51), Count: 2, HasCount: true})
	got = g.Pending()
	if len(got) != 3 || got[2].Kind != GapDumpTruncated || got[0].Kind != GapSourceOverrun || !got[0].From.Equal(at(0)) || !got[0].To.Equal(at(11)) {
		t.Fatalf("after a new kind = %+v", got)
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
