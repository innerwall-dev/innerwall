package collect

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

var (
	src1 = netip.MustParseAddr("10.0.0.20")
	src2 = netip.MustParseAddr("10.0.0.21")
	dst  = netip.MustParseAddr("10.0.0.10")
	t0   = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
)

func obs(at time.Time, src netip.Addr, port uint16, conns, bytes uint64) Observation {
	return Observation{At: at, Src: src, Dst: dst, DstPort: port, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, Connections: conns, Bytes: bytes}
}

// TestAggregatorCollapsesKeys checks that observations sharing a key fold
// into one record with summed counters and the earliest and latest
// instants, that distinct keys stay distinct, that the ephemeral source
// port plays no part (there is none in the observation), and that a
// closed window is ordered deterministically and leaves a fresh one open
// at its end.
func TestAggregatorCollapsesKeys(t *testing.T) {
	a := NewAggregator(t0)
	a.Add(obs(t0.Add(5*time.Second), src1, 5432, 1, 0))
	a.Add(obs(t0.Add(2*time.Second), src1, 5432, 1, 0))
	a.Add(obs(t0.Add(9*time.Second), src1, 5432, 0, 4096))
	a.Add(obs(t0.Add(3*time.Second), src2, 5432, 1, 100))
	a.Add(obs(t0.Add(4*time.Second), src1, 443, 1, 0))
	// Same tuple, different decision: a distinct key.
	o := obs(t0.Add(6*time.Second), src1, 5432, 1, 0)
	o.Decision = innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK
	a.Add(o)
	if a.Len() != 4 {
		t.Fatalf("open keys = %d, want 4", a.Len())
	}

	end := t0.Add(time.Minute)
	w := a.Close(end)
	if w.Start != t0 || w.End != end {
		t.Fatalf("window bounds = %v..%v", w.Start, w.End)
	}
	if len(w.Records) != 4 {
		t.Fatalf("records = %d, want 4", len(w.Records))
	}
	// Ordered by src, dst, port, protocol, decision.
	r := w.Records[0]
	if r.GetSrcAddress() != src1.String() || r.GetDstPort() != 443 {
		t.Fatalf("first record = %v", r)
	}
	r = w.Records[1]
	if r.GetDstPort() != 5432 || r.GetDecision() != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatalf("second record = %v", r)
	}
	if r.GetConnectionCount() != 2 || r.GetByteCount() != 4096 {
		t.Fatalf("collapsed counters = %d/%d", r.GetConnectionCount(), r.GetByteCount())
	}
	if !r.GetFirstSeen().AsTime().Equal(t0.Add(2*time.Second)) || !r.GetLastSeen().AsTime().Equal(t0.Add(9*time.Second)) {
		t.Fatalf("seen = %v..%v", r.GetFirstSeen().AsTime(), r.GetLastSeen().AsTime())
	}
	if r.GetDirection() != innerwallv1.Direction_DIRECTION_INBOUND {
		t.Fatalf("direction = %v", r.GetDirection())
	}
	if w.Records[2].GetDecision() != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
		t.Fatalf("third record = %v", w.Records[2])
	}
	if w.Records[3].GetSrcAddress() != src2.String() || w.Records[3].GetByteCount() != 100 {
		t.Fatalf("fourth record = %v", w.Records[3])
	}

	// The next window starts where this one ended and is empty.
	if a.Len() != 0 {
		t.Fatalf("keys after close = %d", a.Len())
	}
	next := a.Close(end.Add(time.Minute))
	if next.Start != end || next.Len() != 0 {
		t.Fatalf("next window = %+v", next)
	}
}

func window(start time.Time, n int) *Window {
	w := &Window{Start: start, End: start.Add(time.Minute)}
	for i := 0; i < n; i++ {
		w.Records = append(w.Records, &innerwallv1.FlowRecord{DstPort: uint32(i)}) //nolint:gosec // small
	}
	return w
}

// TestAggregatorBoundsOpenWindow is the regression for an open window
// that grew with every distinct key: past its bound an observation for a
// new key is dropped, an observation for a key already held still folds
// in, and the window records one window_overflow gap at its close, from
// the first drop to the close, counting the observations dropped. The
// next window starts empty, with nothing dropped.
func TestAggregatorBoundsOpenWindow(t *testing.T) {
	gaps := &Gaps{}
	a := NewBoundedAggregator(t0, 3)
	a.Gaps = gaps
	addr := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{10, 1, byte(i >> 8), byte(i)}) }
	for i := 0; i < 3; i++ {
		a.Add(obs(t0.Add(time.Second), addr(i), 443, 1, 0))
	}
	// Two new keys past the bound are dropped, the first at t0+5s.
	a.Add(obs(t0.Add(5*time.Second), addr(100), 443, 1, 0))
	a.Add(obs(t0.Add(7*time.Second), addr(101), 443, 1, 0))
	// A key already held folds in.
	a.Add(obs(t0.Add(8*time.Second), addr(0), 443, 0, 900))
	if a.Len() != 3 || a.Dropped() != 2 {
		t.Fatalf("open window keys=%d dropped=%d, want 3 and 2", a.Len(), a.Dropped())
	}
	if gaps.Held() != 0 {
		t.Fatal("a gap was recorded before the window closed")
	}
	w := a.Close(t0.Add(60 * time.Second))
	if w.Len() != 3 {
		t.Fatalf("closed window records = %d", w.Len())
	}
	var bytes uint64
	for _, r := range w.Records {
		bytes += r.GetByteCount()
	}
	if bytes != 900 {
		t.Fatalf("bytes for a held key = %d, want 900", bytes)
	}
	got := gaps.Take()
	want := Gap{Kind: GapWindowOverflow, Source: GapNoSource, From: t0.Add(5 * time.Second), To: t0.Add(60 * time.Second), Count: 2, HasCount: true}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("gaps = %+v, want %+v", got, want)
	}
	if wire := got[0].Wire(); wire.GetKind() != innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_WINDOW_OVERFLOW || wire.GetCount() != 2 {
		t.Fatalf("wire gap = %v", wire)
	}
	if a.Len() != 0 || a.Dropped() != 0 {
		t.Fatalf("next window keys=%d dropped=%d", a.Len(), a.Dropped())
	}
	a.Close(t0.Add(120 * time.Second))
	if gaps.Held() != 0 {
		t.Fatal("a window without drops recorded a gap")
	}
	// The default bound is the closed buffer's figure.
	if DefaultWindowKeys != DefaultBufferRecords {
		t.Fatalf("DefaultWindowKeys = %d, want the buffer's %d", DefaultWindowKeys, DefaultBufferRecords)
	}
}

// TestAggregatorDefaultBound is the audit's reproduction at the default
// bound: one more distinct key than the bound leaves the window at the
// bound.
func TestAggregatorDefaultBound(t *testing.T) {
	a := NewAggregator(t0)
	for i := 0; i <= DefaultWindowKeys; i++ {
		a.Add(Observation{At: t0, Src: netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), Dst: dst, DstPort: 80, Connections: 1})
	}
	if a.Len() != DefaultWindowKeys || a.Dropped() != 1 {
		t.Fatalf("open window keys=%d dropped=%d, want %d and 1", a.Len(), a.Dropped(), DefaultWindowKeys)
	}
}

// TestBufferDropsOldestOnOverflow checks the bound: windows beyond the
// record capacity evict the oldest windows first, every evicted record is
// counted, a requeued window is the oldest again, and a single window
// larger than the whole capacity is cut to fit.
func TestBufferDropsOldestOnOverflow(t *testing.T) {
	b := NewBuffer(10)
	b.Push(window(t0, 4))
	b.Push(window(t0.Add(time.Minute), 4))
	if b.Len() != 2 || b.Records() != 8 || b.Dropped() != 0 {
		t.Fatalf("after two pushes: len=%d records=%d dropped=%d", b.Len(), b.Records(), b.Dropped())
	}
	b.Push(window(t0.Add(2*time.Minute), 4))
	if b.Len() != 2 || b.Records() != 8 || b.Dropped() != 4 {
		t.Fatalf("after overflow: len=%d records=%d dropped=%d", b.Len(), b.Records(), b.Dropped())
	}
	w, ok := b.Pop(context.Background())
	if !ok || !w.Start.Equal(t0.Add(time.Minute)) {
		t.Fatalf("popped %+v; the oldest surviving window should be the second", w)
	}
	// A failed delivery requeues it at the front.
	b.Requeue(w)
	w2, _ := b.Pop(context.Background())
	if !w2.Start.Equal(t0.Add(time.Minute)) {
		t.Fatalf("requeued window not popped first: %v", w2.Start)
	}
	b.Pop(context.Background())
	if b.Len() != 0 {
		t.Fatalf("len after draining = %d", b.Len())
	}

	// One oversized window is cut to capacity, dropping its oldest
	// records (the ones at the front).
	b.Push(window(t0, 25))
	if b.Records() != 10 || b.Dropped() != 4+15 {
		t.Fatalf("oversized push: records=%d dropped=%d", b.Records(), b.Dropped())
	}
	w3, _ := b.Pop(context.Background())
	if w3.Records[0].GetDstPort() != 15 {
		t.Fatalf("oversized window kept the wrong records: first port %d", w3.Records[0].GetDstPort())
	}

	// Empty windows are never queued; Pop honors ctx.
	b.Push(window(t0, 0))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, ok := b.Pop(ctx); ok {
		t.Fatal("popped an empty window")
	}
}

// TestBatchesCapRecords checks that a window is split into requests of at
// most the cap, each carrying the window bounds.
func TestBatchesCapRecords(t *testing.T) {
	w := window(t0, 7)
	reqs := Batches(w, 3)
	if len(reqs) != 3 || len(reqs[0].GetRecords()) != 3 || len(reqs[2].GetRecords()) != 1 {
		t.Fatalf("batches = %v", reqs)
	}
	for _, r := range reqs {
		if !r.GetWindowStart().AsTime().Equal(t0) || !r.GetWindowEnd().AsTime().Equal(t0.Add(time.Minute)) {
			t.Fatalf("batch bounds = %v..%v", r.GetWindowStart().AsTime(), r.GetWindowEnd().AsTime())
		}
	}
	// An empty window, queued to carry gaps, is one request with its
	// bounds and no records.
	if reqs := Batches(window(t0, 0), 3); len(reqs) != 1 || len(reqs[0].GetRecords()) != 0 || !reqs[0].GetWindowStart().AsTime().Equal(t0) {
		t.Fatalf("empty window batches = %v", reqs)
	}
}

// fakeSource emits a fixed set of observations then blocks until ctx ends.
type fakeSource struct {
	obs []Observation
}

func (f *fakeSource) Run(ctx context.Context, emit func(Observation)) error {
	for _, o := range f.obs {
		emit(o)
	}
	<-ctx.Done()
	return nil
}

// TestCollectorRotatesWindows runs the collector with a short window and
// checks that observations land in a closed window in the buffer and that
// the window length follows SyncConfig.
func TestCollectorRotatesWindows(t *testing.T) {
	buf := NewBuffer(0)
	c := &Collector{Source: &fakeSource{obs: []Observation{obs(time.Now(), src1, 22, 1, 0), obs(time.Now(), src1, 22, 1, 10)}}, Buffer: buf}
	c.window.Store(int64(30 * time.Millisecond))
	if c.Window() != 30*time.Millisecond {
		t.Fatalf("window = %v", c.Window())
	}
	c.SetConfig(&innerwallv1.SyncConfig{FlowAggregationWindowSeconds: 0})
	if c.Window() != 30*time.Millisecond {
		t.Fatal("a zero configured window replaced the current one")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = c.Run(ctx); close(done) }()
	w, ok := buf.Pop(ctx)
	if !ok {
		t.Fatal("no window")
	}
	if w.Len() != 1 || w.Records[0].GetConnectionCount() != 2 || w.Records[0].GetByteCount() != 10 {
		t.Fatalf("window records = %v", w.Records)
	}
	if !w.End.After(w.Start) {
		t.Fatalf("window bounds = %v..%v", w.Start, w.End)
	}
	c.SetConfig(&innerwallv1.SyncConfig{FlowAggregationWindowSeconds: 7})
	if c.Window() != 7*time.Second {
		t.Fatalf("window after config = %v", c.Window())
	}
	cancel()
	<-done
}

// TestCloseBounds checks where a closing window ends and the next starts.
// In step, both are now. A wall clock stepped back while the window was
// open cuts the window at its own clock, start plus its monotonic length,
// and opens the next on the stepped clock, so the bounds never invert. A
// fall within slewing's reach is no step; one that would still invert the
// bounds of a window closed moments after it opened holds the end at the
// start. A forward step is left alone.
func TestCloseBounds(t *testing.T) {
	cases := []struct {
		name      string
		now       time.Time
		elapsed   time.Duration
		end, next time.Time
		step      time.Duration
	}{
		{"in step", t0.Add(time.Minute), time.Minute, t0.Add(time.Minute), t0.Add(time.Minute), 0},
		{"stepped back 30m", t0.Add(time.Minute - 30*time.Minute), time.Minute, t0.Add(time.Minute), t0.Add(time.Minute - 30*time.Minute), -30 * time.Minute},
		{"stepped back past the start, closed early", t0.Add(-5 * time.Second), 100 * time.Millisecond, t0.Add(100 * time.Millisecond), t0.Add(-5 * time.Second), -5*time.Second - 100*time.Millisecond},
		{"slewed back within tolerance", t0.Add(time.Minute - 500*time.Millisecond), time.Minute, t0.Add(time.Minute - 500*time.Millisecond), t0.Add(time.Minute - 500*time.Millisecond), 0},
		{"a small fall in a window closed at once", t0.Add(-200 * time.Millisecond), 50 * time.Millisecond, t0, t0, 0},
		{"stepped forward", t0.Add(31 * time.Minute), time.Minute, t0.Add(31 * time.Minute), t0.Add(31 * time.Minute), 0},
	}
	for _, c := range cases {
		end, next, step := closeBounds(t0, c.now, c.elapsed)
		if !end.Equal(c.end) || !next.Equal(c.next) || step != c.step {
			t.Errorf("%s: end %v next %v step %v, want end %v next %v step %v", c.name, end, next, step, c.end, c.next, c.step)
		}
		if end.Before(t0) {
			t.Errorf("%s: end %v before start %v", c.name, end, t0)
		}
	}
}

// TestAggregatorCutHoldsInstantsWithinBounds checks a cut window: it ends
// where it is told, the next window opens at the stepped clock, and the
// instants the stepped clock stamped on its observations, and on its
// first overflow drop, are held within its bounds.
func TestAggregatorCutHoldsInstantsWithinBounds(t *testing.T) {
	gaps := &Gaps{}
	a := NewBoundedAggregator(t0, 1)
	a.Gaps = gaps
	stepped := t0.Add(-30 * time.Minute)
	a.Add(obs(t0.Add(10*time.Second), src1, 22, 1, 0))
	a.Add(obs(stepped.Add(20*time.Second), src1, 22, 1, 0))
	a.Add(obs(stepped.Add(25*time.Second), src2, 22, 1, 0)) // past the bound: dropped
	end := t0.Add(time.Minute)
	w := a.Cut(end, stepped.Add(30*time.Second))
	if !w.Start.Equal(t0) || !w.End.Equal(end) || w.Len() != 1 {
		t.Fatalf("cut window = %v..%v with %d records", w.Start, w.End, w.Len())
	}
	r := w.Records[0]
	if !r.GetFirstSeen().AsTime().Equal(t0) || !r.GetLastSeen().AsTime().Equal(t0.Add(10*time.Second)) || r.GetConnectionCount() != 2 {
		t.Fatalf("record seen %v..%v, connections %d", r.GetFirstSeen().AsTime(), r.GetLastSeen().AsTime(), r.GetConnectionCount())
	}
	if got := gaps.Pending(); len(got) != 1 || !got[0].From.Equal(t0) || !got[0].To.Equal(end) {
		t.Fatalf("overflow gap = %+v, want %v..%v", got, t0, end)
	}
	if !a.Start().Equal(stepped.Add(30 * time.Second)) {
		t.Fatalf("next window starts %v, want the stepped clock", a.Start())
	}
}

// stepSource steps the clocks once the collector has opened its first
// window, then emits observations stamped with the stepped clock.
type stepSource struct {
	step func()
	obs  []Observation
}

func (s *stepSource) Run(ctx context.Context, emit func(Observation)) error {
	s.step()
	for _, o := range s.obs {
		emit(o)
	}
	<-ctx.Done()
	return nil
}

// TestCollectorCutsAWindowAtABackwardsStep is the regression for a wall
// clock stepped back while a window was open: the collector stamped the
// window's end from the stepped clock, so it ended before it started and
// the control plane refused it. The window is now cut at the step: it
// ends at its start plus its monotonic length, its records are kept, and
// it says why in a WARN line.
func TestCollectorCutsAWindowAtABackwardsStep(t *testing.T) {
	var mu sync.Mutex
	wall, mono := t0, time.Duration(0)
	stepped := t0.Add(-30 * time.Minute)
	var logged bytes.Buffer
	buf := NewBuffer(0)
	c := &Collector{
		Buffer: buf,
		Log:    slog.New(slog.NewTextHandler(&lockedWriter{w: &logged}, &slog.HandlerOptions{Level: slog.LevelWarn})),
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return wall
		},
		Monotonic: func() time.Duration {
			mu.Lock()
			defer mu.Unlock()
			return mono
		},
		Source: &stepSource{
			step: func() {
				mu.Lock()
				defer mu.Unlock()
				wall, mono = stepped.Add(time.Minute), time.Minute
			},
			obs: []Observation{obs(stepped.Add(30*time.Second), src1, 22, 1, 0)},
		},
	}
	c.window.Store(int64(30 * time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = c.Run(ctx); close(done) }()
	w, ok := buf.Pop(ctx)
	if !ok {
		t.Fatal("no window")
	}
	cancel()
	<-done
	if !w.Start.Equal(t0) || !w.End.Equal(t0.Add(time.Minute)) {
		t.Fatalf("window = %v..%v, want %v..%v", w.Start, w.End, t0, t0.Add(time.Minute))
	}
	if w.Len() != 1 || !w.Records[0].GetFirstSeen().AsTime().Equal(t0) {
		t.Fatalf("records = %v", w.Records)
	}
	for _, want := range []string{"level=WARN", "wall clock stepped back", "step=-30m0s"} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("log %q lacks %q", logged.String(), want)
		}
	}
}

// lockedWriter serializes writes from the collector's goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
