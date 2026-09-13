package collect

import (
	"context"
	"net/netip"
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
	if len(Batches(window(t0, 0), 3)) != 0 {
		t.Fatal("empty window produced a request")
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
