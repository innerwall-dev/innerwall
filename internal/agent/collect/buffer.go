package collect

import (
	"context"
	"sync"
	"sync/atomic"
)

// DefaultBufferRecords is the buffer capacity in records unless
// configured otherwise: an hour of busy windows in memory, and a bound the
// agent enforces on itself before it would ever burden the host
// (ADR-0011).
const DefaultBufferRecords = 50_000

// DefaultNetlinkBuffer is the receive buffer a flow source asks for on its
// netlink socket unless configured otherwise: enough for a burst of tens
// of thousands of events while the source is busy, at a few megabytes of
// kernel memory per socket (ADR-0011).
const DefaultNetlinkBuffer = 4 << 20

// Buffer holds closed windows between the collector and the reporter. It
// is bounded in records; when a window does not fit, the oldest windows
// are dropped until it does and every dropped record is counted, so the
// heartbeat can tell the operator the flow map is incomplete.
type Buffer struct {
	// Gaps records every overflow as a buffer_overflow gap over the
	// dropped window's bounds, with the records dropped; nil records
	// nothing.
	Gaps *Gaps

	mu       sync.Mutex
	windows  []*Window
	records  int
	capacity int
	dropped  atomic.Uint64
	ready    chan struct{}
}

// NewBuffer returns a buffer holding at most capacity records;
// DefaultBufferRecords when capacity is not positive.
func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultBufferRecords
	}
	return &Buffer{capacity: capacity, ready: make(chan struct{}, 1)}
}

// Push appends w as the newest window. An empty window is not queued;
// Carry queues one when gaps need it.
func (b *Buffer) Push(w *Window) {
	if w == nil || w.Len() == 0 {
		return
	}
	b.mu.Lock()
	b.windows = append(b.windows, w)
	b.records += w.Len()
	b.trim()
	b.mu.Unlock()
	b.signal()
}

// Requeue puts w back as the oldest window after a failed delivery.
func (b *Buffer) Requeue(w *Window) {
	if w == nil || w.Len() == 0 {
		return
	}
	b.mu.Lock()
	b.windows = append([]*Window{w}, b.windows...)
	b.records += w.Len()
	b.trim()
	b.mu.Unlock()
	b.signal()
}

// trim drops oldest windows until the buffer fits its capacity. A single
// window larger than the whole capacity is cut to fit, dropping its oldest
// records; the caller holds the lock.
func (b *Buffer) trim() {
	for b.records > b.capacity && len(b.windows) > 1 {
		oldest := b.windows[0]
		b.windows = b.windows[1:]
		b.records -= oldest.Len()
		b.overflow(oldest, oldest.Len())
	}
	if b.records > b.capacity && len(b.windows) == 1 {
		w := b.windows[0]
		excess := b.records - b.capacity
		w.Records = w.Records[excess:]
		b.records -= excess
		b.overflow(w, excess)
	}
}

// overflow counts n records dropped from w and records the loss over w's
// bounds; the caller holds the lock.
func (b *Buffer) overflow(w *Window, n int) {
	if n <= 0 {
		return
	}
	b.dropped.Add(uint64(n))
	b.Gaps.Record(Gap{Kind: GapBufferOverflow, Source: GapNoSource, From: w.Start, To: w.End, Count: uint64(n), HasCount: true})
}

// Carry queues an empty window, unless a window is already queued, so
// that gaps waiting to be shipped ride it when no records are.
func (b *Buffer) Carry(w *Window) {
	if w == nil {
		return
	}
	b.mu.Lock()
	if len(b.windows) > 0 {
		b.mu.Unlock()
		return
	}
	b.windows = append(b.windows, w)
	b.mu.Unlock()
	b.signal()
}

func (b *Buffer) signal() {
	select {
	case b.ready <- struct{}{}:
	default:
	}
}

// Pop removes and returns the oldest window, waiting until one is queued
// or ctx ends.
func (b *Buffer) Pop(ctx context.Context) (*Window, bool) {
	for {
		b.mu.Lock()
		if len(b.windows) > 0 {
			w := b.windows[0]
			b.windows = b.windows[1:]
			b.records -= w.Len()
			b.mu.Unlock()
			return w, true
		}
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false
		case <-b.ready:
		}
	}
}

// Len returns the number of queued windows.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.windows)
}

// Records returns the number of queued records.
func (b *Buffer) Records() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.records
}

// Dropped returns the number of records dropped to overflow since the
// buffer was created. It is what the heartbeat reports.
func (b *Buffer) Dropped() uint64 { return b.dropped.Load() }
