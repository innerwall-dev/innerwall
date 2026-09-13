package collect

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// DefaultBatchMax caps the records in one ReportFlowsRequest before the
// control plane has said otherwise in HelloAck.
const DefaultBatchMax = 1000

// Dialer opens a connection to the control plane with the workload
// credential. The reporter dials for itself so that telemetry never shares
// a connection's fate with the sync stream, and reconnects with its own
// backoff (ADR-0015).
type Dialer func(ctx context.Context) (innerwallv1.AgentServiceClient, io.Closer, error)

// Reporter ships closed windows over ReportFlows: one stream per window,
// one request per batch of at most the configured records, closed for the
// accepted count. A window whose stream fails is requeued and retried
// after a jittered backoff; the buffer's bound decides what is dropped if
// the control plane stays away.
type Reporter struct {
	Buffer *Buffer
	Dial   Dialer
	Log    *slog.Logger
	// BackoffBase and BackoffCap bound the wait after a failed report:
	// one second and five minutes when zero.
	BackoffBase time.Duration
	BackoffCap  time.Duration
	// SendTimeout bounds one window's stream; 30 seconds when zero.
	SendTimeout time.Duration
	// Now is the clock; time.Now if nil.
	Now func() time.Time

	batchMax atomic.Int64
}

func (r *Reporter) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reporter) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// SetConfig adopts the control plane's batch cap.
func (r *Reporter) SetConfig(cfg *innerwallv1.SyncConfig) {
	if n := cfg.GetFlowBatchMaxRecords(); n > 0 {
		r.batchMax.Store(int64(n))
	}
}

// BatchMax returns the current batch cap.
func (r *Reporter) BatchMax() int {
	if n := r.batchMax.Load(); n > 0 {
		return int(n)
	}
	return DefaultBatchMax
}

// Run reports until ctx ends.
func (r *Reporter) Run(ctx context.Context) error {
	base, limit := r.BackoffBase, r.BackoffCap
	if base <= 0 {
		base = time.Second
	}
	if limit <= 0 {
		limit = 5 * time.Minute
	}
	rng := rand.New(rand.NewPCG(uint64(r.now().UnixNano()), 3)) //nolint:gosec // jitter, not secrecy
	attempt := 0
	for {
		w, ok := r.Buffer.Pop(ctx)
		if !ok {
			return nil
		}
		accepted, err := r.report(ctx, w)
		if err == nil {
			attempt = 0
			r.log().Info("flow window reported", "start", w.Start, "end", w.End, "records", w.Len(), "accepted", accepted)
			continue
		}
		if ctx.Err() != nil {
			r.Buffer.Requeue(w)
			return nil
		}
		attempt++
		wait := backoff(attempt, base, limit, rng)
		r.log().Warn("flow report failed; will retry", "error", err, "records", w.Len(), "attempt", attempt, "wait", wait, "buffered_windows", r.Buffer.Len())
		r.Buffer.Requeue(w)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// report ships one window and returns the control plane's accepted count.
func (r *Reporter) report(ctx context.Context, w *Window) (uint64, error) {
	timeout := r.SendTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, closer, err := r.Dial(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = closer.Close() }()
	stream, err := client.ReportFlows(ctx)
	if err != nil {
		return 0, fmt.Errorf("collect: opening report stream: %w", err)
	}
	for _, req := range Batches(w, r.BatchMax()) {
		if err := stream.Send(req); err != nil {
			return 0, fmt.Errorf("collect: sending window: %w", err)
		}
	}
	res, err := stream.CloseAndRecv()
	if err != nil {
		return 0, fmt.Errorf("collect: closing report stream: %w", err)
	}
	return res.GetAcceptedRecords(), nil
}

// Batches splits a window into requests of at most batchMax records, each
// carrying the window's bounds.
func Batches(w *Window, batchMax int) []*innerwallv1.ReportFlowsRequest {
	if batchMax <= 0 {
		batchMax = DefaultBatchMax
	}
	var out []*innerwallv1.ReportFlowsRequest
	for i := 0; i < len(w.Records); i += batchMax {
		end := min(i+batchMax, len(w.Records))
		out = append(out, &innerwallv1.ReportFlowsRequest{
			WindowStart: timestamppb.New(w.Start),
			WindowEnd:   timestamppb.New(w.End),
			Records:     w.Records[i:end],
		})
	}
	return out
}
