package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/rendered"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

var _ compiler.Store = (*Store)(nil)

// PolicyChannel is the notification channel a render announces changed
// workload policies on. The payload is "<workload-id> <version>".
const PolicyChannel = "innerwall_policy"

// renderLockKey is the advisory lock every render transaction takes, so
// that renders in any two processes (the control plane and the command
// line, or two replicas) are serialized (ADR-0017).
const renderLockKey int64 = 0x1_4e4e_4552_5741 // arbitrary, fixed

// RenderTx implements compiler.Store.
func (s *Store) RenderTx(ctx context.Context, fn func(ctx context.Context, tx compiler.Tx) error) error {
	return s.tx(ctx, func(q *db.Queries) error {
		if err := q.AcquireRenderLock(ctx, renderLockKey); err != nil {
			return fmt.Errorf("store: acquiring render lock: %w", err)
		}
		return fn(ctx, &renderTx{q: q})
	})
}

type renderTx struct {
	q *db.Queries
}

// LoadInputs implements compiler.Tx.
func (t *renderTx) LoadInputs(ctx context.Context) (*compiler.Inputs, error) {
	workloads, err := listWorkloads(ctx, t.q)
	if err != nil {
		return nil, err
	}
	rulesets, err := listRulesets(ctx, t.q)
	if err != nil {
		return nil, err
	}
	services, err := listServices(ctx, t.q)
	if err != nil {
		return nil, err
	}
	groups, err := listAddressGroups(ctx, t.q)
	if err != nil {
		return nil, err
	}
	return &compiler.Inputs{Workloads: workloads, Rulesets: rulesets, Services: services, AddressGroups: groups}, nil
}

// LoadPolicies implements compiler.Tx.
func (t *renderTx) LoadPolicies(ctx context.Context) (map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, error) {
	rows, err := t.q.ListWorkloadPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing policies: %w", err)
	}
	out := make(map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, len(rows))
	for _, r := range rows {
		p, err := rendered.Unmarshal(r.Policy)
		if err != nil {
			return nil, fmt.Errorf("store: policy of %s: %w", r.WorkloadID, err)
		}
		p.Version = uint64(r.Version) //nolint:gosec // versions are non-negative
		out[identity.FromUUID(r.WorkloadID)] = p
	}
	return out, nil
}

// SavePolicy implements compiler.Tx.
func (t *renderTx) SavePolicy(ctx context.Context, id identity.WorkloadID, policy *innerwallv1.WorkloadPolicy, now time.Time) error {
	b, err := rendered.Marshal(policy)
	if err != nil {
		return err
	}
	if err := t.q.UpsertWorkloadPolicy(ctx, db.UpsertWorkloadPolicyParams{WorkloadID: id.UUID(), Version: int64(policy.GetVersion()), Policy: b, RenderedAt: now}); err != nil { //nolint:gosec // versions are small
		return fmt.Errorf("store: saving policy: %w", err)
	}
	if err := t.q.NotifyPolicyChanged(ctx, db.NotifyPolicyChangedParams{Channel: PolicyChannel, Payload: id.String() + " " + strconv.FormatUint(policy.GetVersion(), 10)}); err != nil {
		return fmt.Errorf("store: announcing policy: %w", err)
	}
	return nil
}

// GetWorkloadPolicy returns the persisted policy of one workload with its
// version, or nil when none has been rendered yet.
func (s *Store) GetWorkloadPolicy(ctx context.Context, id identity.WorkloadID) (*innerwallv1.WorkloadPolicy, error) {
	row, err := s.q.GetWorkloadPolicy(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up policy: %w", err)
	}
	p, err := rendered.Unmarshal(row.Policy)
	if err != nil {
		return nil, err
	}
	p.Version = uint64(row.Version) //nolint:gosec // versions are non-negative
	return p, nil
}

// ListenPolicyChanges delivers every render announcement to fn until ctx
// ends. It holds one dedicated connection outside the pool; if that
// connection fails it reconnects with jittered backoff and calls onReady
// again, because announcements made while it was down are gone and the
// caller must reconcile against persisted state. onReady is also called
// once the first LISTEN is in place, so a caller can register streams only
// after it can hear about their changes.
func (s *Store) ListenPolicyChanges(ctx context.Context, log *slog.Logger, onReady func(), fn func(compiler.Announcement)) error {
	if log == nil {
		log = slog.Default()
	}
	attempt := 0
	for {
		err := s.listenOnce(ctx, onReady, fn)
		if ctx.Err() != nil {
			return nil
		}
		attempt++
		wait := backoff(attempt)
		log.Error("policy listener lost; reconnecting", "error", err, "retry_in", wait)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

func (s *Store) listenOnce(ctx context.Context, onReady func(), fn func(compiler.Announcement)) error {
	pooled, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("store: acquiring listener connection: %w", err)
	}
	conn := pooled.Hijack()
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(ctx, "LISTEN "+PolicyChannel); err != nil {
		return fmt.Errorf("store: listen: %w", err)
	}
	if onReady != nil {
		onReady()
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("store: waiting for notification: %w", err)
		}
		idText, verText, ok := strings.Cut(n.Payload, " ")
		if !ok {
			continue
		}
		id, err := identity.ParseWorkloadID(idText)
		if err != nil {
			continue
		}
		ver, err := strconv.ParseUint(verText, 10, 64)
		if err != nil {
			continue
		}
		fn(compiler.Announcement{ID: id, Version: ver})
	}
}

// backoff is exponential with full jitter (ADR-0002): random in
// [0, min(cap, base × 2^attempt)].
func backoff(attempt int) time.Duration {
	const base, limit = 500 * time.Millisecond, 30 * time.Second
	ceiling := base << uint(min(attempt, 10)) //nolint:gosec // bounded
	if ceiling > limit || ceiling <= 0 {
		ceiling = limit
	}
	return time.Duration(rand.Int64N(int64(ceiling))) //nolint:gosec // jitter, not secrecy
}
