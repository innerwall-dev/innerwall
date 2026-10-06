package nft

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// ApplyKind says how the last apply reached the kernel.
type ApplyKind string

// Apply kinds.
const (
	ApplyNone  ApplyKind = ""
	ApplyFull  ApplyKind = "full"
	ApplyDelta ApplyKind = "delta"
)

// Config configures a Store.
type Config struct {
	// Runner executes nft; ExecRunner{} when nil.
	Runner Runner
	// StateDir holds the persisted policy file and the mark allocation.
	StateDir string
	// Table and NflogGroup parameterize the rendering.
	Table      string
	NflogGroup uint16
	Log        *slog.Logger
}

// Store is the enforce.PolicyStore over the owned nftables table. Every
// Apply persists the policy to disk first, durably, and only then runs
// one nft transaction, so a policy the agent acknowledges always has its
// persisted copy and the next daemon start re-applies it before
// connecting (ADR-0011, ADR-0020). The kernel rules outlive the daemon:
// nothing here removes them on exit, and only Teardown does so at all.
type Store struct {
	runner    Runner
	file      enforce.PolicyFile
	marksPath string
	opts      Options
	log       *slog.Logger

	mu sync.RWMutex
	// current is the policy the kernel holds: it changes only when a
	// transaction succeeds.
	current *innerwallv1.WorkloadPolicy
	alloc   *Allocator
	last    ApplyKind
}

var _ enforce.PolicyStore = (*Store)(nil)

// New builds a store. Nothing is read or applied until Load and Restore.
func New(cfg Config) *Store {
	if cfg.Runner == nil {
		cfg.Runner = ExecRunner{}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Store{
		runner:    cfg.Runner,
		file:      enforce.PolicyFile{Path: enforce.PolicyPath(cfg.StateDir)},
		marksPath: MarksPath(cfg.StateDir),
		opts:      Options{Table: cfg.Table, NflogGroup: cfg.NflogGroup},
		log:       cfg.Log,
		alloc:     NewAllocator(),
	}
}

// Load reads the persisted policy and mark allocation into the store
// without touching the kernel. A missing policy file leaves the store
// empty. A corrupt policy file is an error and nothing is loaded from it:
// the caller reports it and carries on with whatever the kernel already
// holds, never a partial apply.
//
// The allocation is saved before the policy, so it covers every rule of
// the policy beside it. When it is missing (a state directory written
// before allocation was persisted, which numbered rules by canonical
// position) it is seeded from the policy, which reproduces exactly that
// numbering. When it is corrupt it is seeded the same way and the loss is
// logged: the policy is still restored, and only connections marked
// before the restart can be misattributed.
func (s *Store) Load() error {
	p, err := s.file.Load()
	if err != nil {
		return err
	}
	alloc, err := LoadAllocator(s.marksPath)
	if err != nil {
		s.log.Error("mark allocation unreadable; seeding it from the persisted policy, and connections marked before this start may be misattributed", "error", err)
		alloc = nil
	}
	if alloc == nil {
		alloc = NewAllocator()
	}
	if p != nil {
		if _, err := alloc.Allocate(p, p); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alloc = alloc
	if p != nil {
		s.current = p
	}
	return nil
}

// Restore installs the loaded policy as a full rendering, so that after a
// reboot the kernel holds what the agent last acknowledged before the
// control plane is even dialed. It is a no-op with nothing loaded.
func (s *Store) Restore(ctx context.Context) error {
	s.mu.RLock()
	p := s.current
	alloc := s.alloc.Clone()
	s.mu.RUnlock()
	if p == nil {
		return nil
	}
	marks, err := alloc.Allocate(p, p)
	if err != nil {
		return err
	}
	script, err := Render(p, marks, s.opts)
	if err != nil {
		return err
	}
	if err := s.runner.Apply(ctx, script); err != nil {
		return err
	}
	s.mu.Lock()
	s.last = ApplyFull
	s.mu.Unlock()
	s.log.Info("persisted policy re-applied", "version", p.GetVersion(), "mode", p.GetMode(), "rules", len(p.GetInboundRules()))
	return nil
}

// Apply implements enforce.PolicyStore. The policy is persisted first:
// its rules are given their stable mark values, and the allocation and
// then the policy are written durably to the state directory. Only then
// does the kernel see it. A change that is peer membership only is applied
// as set element updates; anything else is a full rendering. Both are
// single transactions, and a failed set update falls back to a full
// rendering once before the apply is reported failed.
//
// A persistence failure fails the apply with the kernel untouched, so the
// installed policy, the persisted one, and the last acknowledgement all
// still agree. A kernel failure after the policy was persisted fails the
// apply with the file one version ahead of the kernel. That is benign
// under the desired-state model: the file is the desired state, applying
// it again is idempotent, the control plane answers the failure with a
// snapshot, and a reboot inside the window restores the desired version
// rather than an older one (ADR-0020).
func (s *Store) Apply(ctx context.Context, policy *innerwallv1.WorkloadPolicy) error {
	next := rendered.Canonical(policy)
	if len(next.GetInboundRules()) > MaxRules {
		return fmt.Errorf("%w: %d rules, at most %d", ErrTooManyRules, len(next.GetInboundRules()), MaxRules)
	}
	s.mu.RLock()
	current := s.current
	alloc := s.alloc.Clone()
	s.mu.RUnlock()
	marks, err := alloc.Allocate(next, current)
	if err != nil {
		return err
	}
	full, err := Render(next, marks, s.opts)
	if err != nil {
		return err
	}

	// Persist, durably, before the kernel is touched: the allocation first,
	// because it only ever grows a policy's coverage, then the policy.
	if err := alloc.Save(s.marksPath); err != nil {
		return fmt.Errorf("persisting the mark allocation; nothing was applied: %w", err)
	}
	s.mu.Lock()
	s.alloc = alloc
	s.mu.Unlock()
	if err := s.file.Save(next); err != nil {
		return fmt.Errorf("persisting the policy; nothing was applied: %w", err)
	}

	kind := ApplyFull
	if current != nil {
		if script, ok := RenderDelta(current, next, s.opts); ok {
			if err := s.runner.Apply(ctx, script); err != nil {
				s.log.Warn("set update refused; applying the full ruleset", "error", err)
			} else {
				kind = ApplyDelta
			}
		}
	}
	if kind == ApplyFull {
		if err := s.runner.Apply(ctx, full); err != nil {
			s.log.Warn("policy persisted but refused by the kernel; the persisted policy is ahead of the kernel until the next apply", "version", next.GetVersion(), "installed_version", current.GetVersion(), "error", err)
			return err
		}
	}
	s.mu.Lock()
	s.current = next
	s.last = kind
	s.mu.Unlock()
	return nil
}

// Current implements enforce.PolicyStore.
func (s *Store) Current() *innerwallv1.WorkloadPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// LastApply reports how the most recent apply reached the kernel.
func (s *Store) LastApply() ApplyKind {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.last
}

// Mode returns the installed policy's mode; visibility before any apply.
func (s *Store) Mode() innerwallv1.EnforcementMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY
	}
	return s.current.GetMode()
}

// Classify maps a connection mark to the decision and rule that admitted
// the connection, reading only the agent's region of the mark and
// resolving it through the stable allocation, never through the installed
// policy's order: a rule's value is ALLOWED by that rule, whichever
// version installed it; the would-block value is WOULD_BLOCK; anything
// else was not evaluated (visibility mode, or a connection older than
// any ruleset) and is OBSERVED. A connection whose rule has since been
// removed is ALLOWED by the removed rule, named by its own id, which the
// installed policy no longer carries: it is never attributed to another
// rule, because a freed value is not reused until the region is
// exhausted. The foreign bits are ignored.
func (s *Store) Classify(mark uint32) (innerwallv1.PolicyDecision, string) {
	index := RuleIndex(mark)
	if index == WouldBlockIndex {
		return innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK, ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id, ok := s.alloc.Rule(index); ok {
		return innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED, id
	}
	return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ""
}

// Mark returns the mark value, in place, the allocation gives a rule, for
// diagnostics and tests.
func (s *Store) Mark(ruleID string) (uint32, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, ok := s.alloc.Mark(ruleID)
	if !ok {
		return 0, false
	}
	return RuleMark(index), true
}

// TerminalDecision is what a packet logged by the terminal rule means
// under the installed mode: BLOCKED when enforced, WOULD_BLOCK when
// simulated. In visibility mode nothing is logged; a stray event is
// reported as unevaluated.
func (s *Store) TerminalDecision() innerwallv1.PolicyDecision {
	switch s.Mode() {
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED:
		return innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION:
		return innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED:
		return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED
	default:
		return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED
	}
}

// List returns the owned table as the kernel holds it.
func (s *Store) List(ctx context.Context) (string, error) {
	return s.runner.List(ctx, s.opts.table())
}

// Teardown deletes the owned table: the local kill switch. It touches no
// other table and leaves the persisted policy in place, so the next daemon
// start re-applies it; stopping enforcement for good means stopping the
// daemon too, and the command that calls this says so (ADR-0011).
func Teardown(ctx context.Context, runner Runner, opts Options) error {
	if runner == nil {
		runner = ExecRunner{}
	}
	if err := runner.Apply(ctx, RenderTeardown(opts)); err != nil {
		return fmt.Errorf("removing the owned table: %w", err)
	}
	return nil
}
