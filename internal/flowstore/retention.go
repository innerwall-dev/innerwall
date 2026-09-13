package flowstore

import (
	"context"
	"log/slog"
	"time"
)

// DefaultRetention is how long windows are kept unless configured
// otherwise. Totals are never pruned.
const DefaultRetention = 30 * 24 * time.Hour

// DefaultRetentionInterval is how often a replica attempts a pruning run.
const DefaultRetentionInterval = time.Hour

// Retention prunes aged windows on a schedule. Every replica runs one; the
// store's advisory lock makes sure only one prunes at a time, and a replica
// that finds the lock held skips the round (ADR-0017). This is a scheduled
// maintenance job with a fixed period, not a poll for work: it runs whether
// or not there is anything to delete, and nothing waits on it.
type Retention struct {
	Store FlowStore
	// Horizon is the age beyond which windows are deleted;
	// DefaultRetention when zero.
	Horizon time.Duration
	// Interval is the period between runs; DefaultRetentionInterval when
	// zero.
	Interval time.Duration
	Log      *slog.Logger
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

func (r *Retention) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Retention) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

func (r *Retention) horizon() time.Duration {
	if r.Horizon > 0 {
		return r.Horizon
	}
	return DefaultRetention
}

// RunOnce performs one pruning run.
func (r *Retention) RunOnce(ctx context.Context) (deleted int64, ran bool, err error) {
	return r.Store.PruneWindows(ctx, r.now().Add(-r.horizon()))
}

// Run prunes once immediately and then every Interval until ctx ends.
func (r *Retention) Run(ctx context.Context) error {
	interval := r.Interval
	if interval <= 0 {
		interval = DefaultRetentionInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		deleted, ran, err := r.RunOnce(ctx)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil
			}
			r.log().Error("flow retention failed", "error", err)
		case ran:
			r.log().Info("flow retention ran", "deleted_windows", deleted, "horizon", r.horizon())
		default:
			r.log().Debug("flow retention skipped; another replica holds the lock")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
