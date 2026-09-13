package compiler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// Tx is the store's side of one render: everything is read and written
// inside a single transaction that holds the render lock.
type Tx interface {
	LoadInputs(ctx context.Context) (*Inputs, error)
	// LoadPolicies returns the persisted policy of every workload that has
	// one, with its version.
	LoadPolicies(ctx context.Context) (map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, error)
	// SavePolicy persists a changed policy at the version it carries and
	// announces the change so the stream holding the workload pushes it
	// once the transaction commits.
	SavePolicy(ctx context.Context, id identity.WorkloadID, policy *innerwallv1.WorkloadPolicy, now time.Time) error
}

// Store runs render transactions.
type Store interface {
	// RenderTx runs fn in one transaction holding the render lock, so two
	// renders in any two processes never interleave. It commits when fn
	// returns nil and rolls back otherwise.
	RenderTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
}

// Announcement is what a render publishes for each workload whose version
// advanced: the store delivers it over the database's notification channel
// to every replica, and the replica holding the workload's stream pushes.
type Announcement struct {
	ID      identity.WorkloadID
	Version uint64
}

// Change is one workload whose version advanced in a render.
type Change struct {
	ID      identity.WorkloadID
	Version uint64
	Changes int
}

// Report summarizes one render.
type Report struct {
	// Workloads is how many workloads were rendered.
	Workloads int
	// Changed lists the workloads whose rendered output changed, and
	// therefore whose version advanced and whose stream is pushed.
	Changed []Change
}

// Engine renders on demand. Callers invoke Render after every change that
// can affect a rendered policy: authored mutations, enrollment, label and
// mode changes, and inventory reports that changed a workload's addresses.
type Engine struct {
	Store Store
	Log   *slog.Logger
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) log() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

// Render re-renders every workload and persists the ones whose output
// changed. It is synchronous: when it returns, every changed version is
// durable and announced.
func (e *Engine) Render(ctx context.Context) (*Report, error) {
	var report *Report
	err := e.Store.RenderTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		report, err = e.RenderIn(ctx, tx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("compiler: render: %w", err)
	}
	return report, nil
}

// RenderIn renders inside a transaction the caller already holds under
// the render lock, for a mutation that must be applied and rendered as
// one unit (a bulk mode change). Changed versions are announced when the
// caller's transaction commits.
func (e *Engine) RenderIn(ctx context.Context, tx Tx) (*Report, error) {
	report := &Report{}
	in, err := tx.LoadInputs(ctx)
	if err != nil {
		return nil, err
	}
	previous, err := tx.LoadPolicies(ctx)
	if err != nil {
		return nil, err
	}
	now := e.now()
	for _, o := range Plan(previous, Render(in)) {
		report.Workloads++
		if !o.Changed {
			continue
		}
		if err := tx.SavePolicy(ctx, o.ID, o.Policy, now); err != nil {
			return nil, err
		}
		report.Changed = append(report.Changed, Change{ID: o.ID, Version: o.Policy.GetVersion(), Changes: len(o.Changes)})
	}
	for _, c := range report.Changed {
		e.log().Info("policy rendered", "workload_id", c.ID, "version", c.Version, "changes", c.Changes)
	}
	return report, nil
}
