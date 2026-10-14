package collect

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Sources runs several sources as one stream of observations. Each
// member is restarted on its own failure with jittered backoff, so one
// source that cannot run (a missing kernel facility) never stops another.
type Sources []Source

// Run implements Source. It returns when ctx ends and never reports an
// error: a member's failures are logged and retried.
func (ss Sources) Run(ctx context.Context, emit func(Observation)) error {
	var wg sync.WaitGroup
	for i, s := range ss {
		wg.Add(1)
		go func(i int, s Source) {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(i))) //nolint:gosec // jitter, not secrecy
			restarter{log: slog.Default(), base: time.Second, limit: time.Minute, rng: rng}.run(ctx, s, emit)
		}(i, s)
	}
	wg.Wait()
	return nil
}

// DefaultWindow is the aggregation window before the control plane has
// said otherwise in HelloAck.
const DefaultWindow = 60 * time.Second

// Collector runs a Source, aggregates what it emits, and closes a window
// into the Buffer every window length. The window length follows
// SyncConfig.flow_aggregation_window_seconds and takes effect at the next
// rotation.
type Collector struct {
	Source Source
	Buffer *Buffer
	// Gaps holds the evidence gaps waiting to be shipped: a window that
	// closes empty is still queued while any are, so they ride it.
	Gaps *Gaps
	Log  *slog.Logger
	// Now is the clock; time.Now if nil.
	Now func() time.Time
	// Monotonic reads a clock that never steps, as the time since any
	// fixed origin; the runtime's monotonic clock if nil. A window's
	// length is measured on it, so a wall clock stepped while a window was
	// open is seen when the window closes.
	Monotonic func() time.Duration
	// WindowKeys bounds the distinct keys one open window holds;
	// DefaultWindowKeys when zero.
	WindowKeys int
	// BackoffBase and BackoffCap bound the wait before a failed source is
	// restarted: one second and one minute when zero.
	BackoffBase time.Duration
	BackoffCap  time.Duration

	window atomic.Int64 // nanoseconds; 0 means DefaultWindow
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Collector) monotonic() func() time.Duration {
	if c.Monotonic != nil {
		return c.Monotonic
	}
	origin := time.Now()
	return func() time.Duration { return time.Since(origin) }
}

func (c *Collector) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

// SetConfig adopts the control plane's aggregation window.
func (c *Collector) SetConfig(cfg *innerwallv1.SyncConfig) {
	if s := cfg.GetFlowAggregationWindowSeconds(); s > 0 {
		c.window.Store(int64(time.Duration(s) * time.Second))
	}
}

// Window returns the current window length.
func (c *Collector) Window() time.Duration {
	if d := c.window.Load(); d > 0 {
		return time.Duration(d)
	}
	return DefaultWindow
}

// Run collects until ctx ends. The source is restarted with jittered
// backoff whenever it fails, because a host without a working source
// still holds its policy and its stream; what it loses is telemetry, and
// that is reported, not fatal. The window open when ctx ends is closed
// and queued so a clean shutdown loses nothing that was observed.
func (c *Collector) Run(ctx context.Context) error {
	base, limit := c.BackoffBase, c.BackoffCap
	if base <= 0 {
		base = time.Second
	}
	if limit <= 0 {
		limit = time.Minute
	}
	rng := rand.New(rand.NewPCG(uint64(c.now().UnixNano()), 2)) //nolint:gosec // jitter, not secrecy

	mono := c.monotonic()
	agg := NewBoundedAggregator(c.now(), c.WindowKeys)
	agg.Gaps = c.Gaps
	opened := mono()
	closeWindow := func() *Window {
		now, at := c.now(), mono()
		start := agg.Start()
		end, next, step := closeBounds(start, now, at-opened)
		opened = at
		// A loss still under way rides this window as far as it has run.
		c.Gaps.CutOpen(end)
		if step == 0 {
			return agg.Close(end)
		}
		c.log().Warn("wall clock stepped back while a flow window was open; window cut at the step",
			"step", step.String(), "start", start, "end", end, "next_start", next)
		return agg.Cut(end, next)
	}
	go restarter{log: c.log(), base: base, limit: limit, rng: rng, now: c.Now}.run(ctx, c.Source, agg.Add)

	timer := time.NewTimer(c.Window())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Buffer.Push(closeWindow())
			return nil
		case <-timer.C:
			w := closeWindow()
			if w.Len() > 0 {
				c.log().Debug("flow window closed", "start", w.Start, "end", w.End, "records", w.Len())
			}
			if w.Len() == 0 && c.Gaps.Held() > 0 {
				c.Buffer.Carry(w)
			} else {
				c.Buffer.Push(w)
			}
			timer.Reset(c.Window())
		}
	}
}

// closeBounds decides where a closing window ends and the next one
// starts, from the window's start, the wall clock now, and the window's
// length on the monotonic clock. Ordinarily both are now.
//
// The wall clock can step back while a window is open (a manual set, a
// time sync correcting by a step rather than a slew), and now would then
// end the window before it started: a window no reader can place, which
// the control plane refuses. When the window's wall-clock length falls
// short of its monotonic length by more than slewing explains, the window
// is cut at the step instead: it ends where its own clock reads now, its
// start plus its monotonic length, so its bounds stay in order and its
// length stays true, and the next window opens on the stepped clock. The
// step, negative, is returned when the window is cut, and zero otherwise.
// Nothing observed is lost in a cut, so no evidence gap is recorded for
// it; the stepped clock is the control plane's to see, in the next
// report's send instant. A smaller fall that would still put the end
// before the start, in a window closed moments after it opened, holds
// the end at the start and the next window opens there too.
//
// A forward step is left alone: the window ends at now, later than its
// monotonic length says, and its bounds stay in order.
func closeBounds(start, now time.Time, elapsed time.Duration) (end, next time.Time, step time.Duration) {
	// On the wall clock alone: subtracting two readings that both carry
	// the monotonic clock would measure the monotonic length again.
	wall := now.Round(0).Sub(start.Round(0))
	if d := wall - elapsed; d < -stepTolerance(elapsed) {
		return start.Add(elapsed), now, d
	}
	if wall < 0 {
		return start, start, 0
	}
	return now, now, 0
}

// stepTolerance is how far a window's wall-clock length may fall short of
// its monotonic length before the difference is a step: a second, plus
// the most a slewing time sync moves the clock over that length, 500
// parts per million.
func stepTolerance(elapsed time.Duration) time.Duration {
	return time.Second + elapsed/2000
}

// Gapped is a source that records the evidence it loses. When such a
// source fails, the restart loop opens a restart gap for it at the
// failure; the source closes it when it next subscribes, so the gap spans
// exactly the time nothing was observed.
type Gapped interface {
	EvidenceGaps() (*Gaps, GapSource)
}

// restarter runs a source until ctx ends, restarting it after every
// failure with jittered backoff.
type restarter struct {
	log         *slog.Logger
	base, limit time.Duration
	rng         *rand.Rand
	// now is the clock and after the wait; time.Now and time.After when
	// nil.
	now   func() time.Time
	after func(time.Duration) <-chan time.Time
}

func (r restarter) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r restarter) run(ctx context.Context, src Source, emit func(Observation)) {
	after := r.after
	if after == nil {
		after = time.After
	}
	attempt := 0
	for {
		started := r.clock()
		err := src.Run(ctx, emit)
		if ctx.Err() != nil {
			return
		}
		failed := r.clock()
		attempt = nextAttempt(attempt, failed.Sub(started), r.limit)
		if g, ok := src.(Gapped); ok {
			gaps, name := g.EvidenceGaps()
			gaps.Open(name, GapSourceRestart, failed)
		}
		wait := backoff(attempt, r.base, r.limit, r.rng)
		r.log.Error("flow source stopped; restarting", "error", err, "attempt", attempt, "wait", wait)
		select {
		case <-ctx.Done():
			return
		case <-after(wait):
		}
	}
}

// nextAttempt is the attempt number after a run that lasted ran. A run
// that stayed up longer than the backoff cap was healthy, so the failure
// that ended it starts the sequence again rather than continuing one that
// recovered long ago.
func nextAttempt(attempt int, ran, limit time.Duration) int {
	if ran > limit {
		attempt = 0
	}
	return attempt + 1
}

// backoff is exponential with full jitter (ADR-0002).
func backoff(attempt int, base, limit time.Duration, rng *rand.Rand) time.Duration {
	ceiling := base << uint(min(attempt, 20)) //nolint:gosec // bounded
	if ceiling > limit || ceiling <= 0 {
		ceiling = limit
	}
	return time.Duration(rng.Int64N(int64(ceiling)))
}
