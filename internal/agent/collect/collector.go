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

	agg := NewBoundedAggregator(c.now(), c.WindowKeys)
	agg.Gaps = c.Gaps
	go restarter{log: c.log(), base: base, limit: limit, rng: rng, now: c.Now}.run(ctx, c.Source, agg.Add)

	timer := time.NewTimer(c.Window())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Buffer.Push(agg.Close(c.now()))
			return nil
		case <-timer.C:
			w := agg.Close(c.now())
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
