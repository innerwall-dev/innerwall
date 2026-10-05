package collect

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// GapKind says how evidence was lost.
type GapKind string

// The ways a source or the buffer can lose evidence. Each is an interval
// in which the flow map is known to be incomplete; what was lost inside
// it cannot be recovered, only bounded in time.
const (
	// GapSourceOverrun: the kernel dropped events because the source's
	// socket receive buffer was full. How many is unknown.
	GapSourceOverrun GapKind = "source_overrun"
	// GapSourceRestart: the source was down between a failure and its
	// next successful subscribe. How many events is unknown.
	GapSourceRestart GapKind = "source_restart"
	// GapBufferOverflow: closed windows were dropped because the buffer
	// was full. The record count is known.
	GapBufferOverflow GapKind = "buffer_overflow"
	// GapDumpTruncated: the table dump at subscribe held more entries than
	// the source processes; the count of skipped entries is known.
	GapDumpTruncated GapKind = "dump_truncated"
)

// GapSource names the source that lost the evidence.
type GapSource string

// The flow sources that record gaps.
const (
	GapConntrack GapSource = "conntrack"
	GapNflog     GapSource = "nflog"
)

// Gap is one interval of known evidence loss, [From, To). Count is the
// number of records or entries lost when HasCount is set; a kernel-side
// loss has no count.
type Gap struct {
	Kind     GapKind
	Source   GapSource
	From, To time.Time
	Count    uint64
	HasCount bool
}

// DefaultGapCapacity bounds the gaps held before they are shipped. Past
// it, gaps of the same kind and source are merged into wider intervals,
// so the bound costs precision, never the fact of a loss (ADR-0011).
const DefaultGapCapacity = 1024

// Gaps records evidence gaps for the agent: closed intervals, and at most
// one open interval per source between a failure and the subscribe that
// ends it. It also counts kernel-side overruns per source. Gaps are
// logged as they are recorded and held, bounded, until they are taken.
type Gaps struct {
	Log *slog.Logger
	// Capacity bounds the held gaps; DefaultGapCapacity when zero.
	Capacity int

	mu       sync.Mutex
	pending  []Gap
	open     map[GapSource]Gap
	overruns atomic.Uint64
}

func (g *Gaps) log() *slog.Logger {
	if g.Log != nil {
		return g.Log
	}
	return slog.Default()
}

func (g *Gaps) capacity() int {
	if g.Capacity > 0 {
		return g.Capacity
	}
	return DefaultGapCapacity
}

// Overrun counts one kernel-side overrun. A nil Gaps counts nothing.
func (g *Gaps) Overrun() {
	if g == nil {
		return
	}
	g.overruns.Add(1)
}

// Overruns returns the overruns counted since the agent started.
func (g *Gaps) Overruns() uint64 {
	if g == nil {
		return 0
	}
	return g.overruns.Load()
}

// Open starts an interval of loss for source at from, unless one is
// already open: the earlier start stands, because nothing was observed
// since. An open interval of another kind is closed at from and the new
// kind takes over, so each recorded gap names what happened. A nil Gaps
// records nothing.
func (g *Gaps) Open(source GapSource, kind GapKind, from time.Time) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open == nil {
		g.open = map[GapSource]Gap{}
	}
	if cur, ok := g.open[source]; ok {
		if cur.Kind == kind {
			return
		}
		cur.To = from
		g.add(cur)
	}
	g.open[source] = Gap{Kind: kind, Source: source, From: from}
}

// Close ends the open interval for source at to, the instant the source
// is observing again, and records it. Nothing happens when none is open.
func (g *Gaps) Close(source GapSource, to time.Time) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	cur, ok := g.open[source]
	if !ok {
		return
	}
	delete(g.open, source)
	cur.To = to
	g.add(cur)
}

// Record records a closed interval.
func (g *Gaps) Record(gap Gap) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.add(gap)
}

// add records gap, merging it into the newest held gap of the same kind
// and source when the intervals touch, and merging older gaps when the
// capacity is reached. The caller holds the lock.
func (g *Gaps) add(gap Gap) {
	if gap.To.Before(gap.From) {
		gap.To = gap.From
	}
	for i := len(g.pending) - 1; i >= 0; i-- {
		p := &g.pending[i]
		if p.Kind != gap.Kind || p.Source != gap.Source {
			continue
		}
		if !gap.From.After(p.To) && !gap.To.Before(p.From) {
			merge(p, gap)
			g.log().Debug("flow evidence gap extended", "kind", p.Kind, "source", p.Source, "from", p.From, "to", p.To)
			return
		}
		break
	}
	attrs := []any{"kind", gap.Kind, "source", gap.Source, "from", gap.From, "to", gap.To}
	if gap.HasCount {
		attrs = append(attrs, "count", gap.Count)
	}
	g.log().Warn("flow evidence gap: the flow map is incomplete in this interval", attrs...)
	if len(g.pending) >= g.capacity() {
		g.compact(gap)
		return
	}
	g.pending = append(g.pending, gap)
}

// compact makes room by merging rather than dropping: the incoming gap
// joins the newest held gap of its kind and source, widening it; failing
// that, the two oldest gaps sharing a kind and source are merged, which
// always exist once the capacity exceeds the number of kinds and sources.
func (g *Gaps) compact(gap Gap) {
	for i := len(g.pending) - 1; i >= 0; i-- {
		if g.pending[i].Kind == gap.Kind && g.pending[i].Source == gap.Source {
			merge(&g.pending[i], gap)
			return
		}
	}
	first := map[[2]string]int{}
	for i, p := range g.pending {
		k := [2]string{string(p.Kind), string(p.Source)}
		j, ok := first[k]
		if !ok {
			first[k] = i
			continue
		}
		merge(&g.pending[j], p)
		g.pending = append(g.pending[:i], g.pending[i+1:]...)
		g.pending = append(g.pending, gap)
		return
	}
	// Capacity below the number of kinds and sources: keep the newest.
	g.pending = append(g.pending[1:], gap)
}

// merge widens p to cover gap and sums the counts; the sum is known only
// when both were.
func merge(p *Gap, gap Gap) {
	if gap.From.Before(p.From) {
		p.From = gap.From
	}
	if gap.To.After(p.To) {
		p.To = gap.To
	}
	p.HasCount = p.HasCount && gap.HasCount
	if p.HasCount {
		p.Count += gap.Count
	} else {
		p.Count = 0
	}
}

// Pending returns the held gaps, oldest first, without taking them.
func (g *Gaps) Pending() []Gap {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Gap(nil), g.pending...)
}
