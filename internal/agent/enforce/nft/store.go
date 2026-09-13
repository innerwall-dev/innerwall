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
	// StateDir holds the persisted policy file.
	StateDir string
	// Table and NflogGroup parameterize the rendering.
	Table      string
	NflogGroup uint16
	Log        *slog.Logger
}

// Store is the enforce.PolicyStore over the owned nftables table. Every
// Apply is one nft transaction; on success the policy is persisted to
// disk so the next daemon start re-applies it before connecting
// (ADR-0011). The kernel rules outlive the daemon: nothing here removes
// them on exit, and only Teardown does so at all.
type Store struct {
	runner Runner
	file   enforce.PolicyFile
	opts   Options
	log    *slog.Logger

	mu      sync.RWMutex
	current *innerwallv1.WorkloadPolicy
	marks   map[uint32]string
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
		runner: cfg.Runner,
		file:   enforce.PolicyFile{Path: enforce.PolicyPath(cfg.StateDir)},
		opts:   Options{Table: cfg.Table, NflogGroup: cfg.NflogGroup},
		log:    cfg.Log,
		marks:  map[uint32]string{},
	}
}

// Load reads the persisted policy into the store without touching the
// kernel. A missing file leaves the store empty. A corrupt file is an
// error and nothing is loaded from it: the caller reports it and carries
// on with whatever the kernel already holds, never a partial apply.
func (s *Store) Load() error {
	p, err := s.file.Load()
	if err != nil {
		return err
	}
	if p == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = p
	s.marks = Marks(p)
	return nil
}

// Restore installs the loaded policy as a full rendering, so that after a
// reboot the kernel holds what the agent last acknowledged before the
// control plane is even dialed. It is a no-op with nothing loaded.
func (s *Store) Restore(ctx context.Context) error {
	s.mu.RLock()
	p := s.current
	s.mu.RUnlock()
	if p == nil {
		return nil
	}
	if err := s.runner.Apply(ctx, Render(p, s.opts)); err != nil {
		return err
	}
	s.mu.Lock()
	s.last = ApplyFull
	s.mu.Unlock()
	s.log.Info("persisted policy re-applied", "version", p.GetVersion(), "mode", p.GetMode(), "rules", len(p.GetInboundRules()))
	return nil
}

// Apply implements enforce.PolicyStore. A change that is peer membership
// only is applied as set element updates; anything else is a full
// rendering. Both are single transactions, and a failed set update falls
// back to a full rendering once before the apply is reported failed. On
// success the policy is persisted; a persistence failure is logged loudly
// but does not fail the apply, because the kernel holds the new state and
// the acknowledgement must describe the kernel.
func (s *Store) Apply(ctx context.Context, policy *innerwallv1.WorkloadPolicy) error {
	next := rendered.Canonical(policy)
	if len(next.GetInboundRules()) > MaxRules {
		return fmt.Errorf("%w: %d rules, at most %d", ErrTooManyRules, len(next.GetInboundRules()), MaxRules)
	}
	s.mu.RLock()
	current := s.current
	s.mu.RUnlock()
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
		if err := s.runner.Apply(ctx, Render(next, s.opts)); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.current = next
	s.marks = Marks(next)
	s.last = kind
	s.mu.Unlock()
	if err := s.file.Save(next); err != nil {
		s.log.Error("policy applied but not persisted; a restart before the next apply will not re-apply it", "version", next.GetVersion(), "error", err)
	}
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

// Classify maps a connection mark to the decision and rule the installed
// ruleset reached, reading only the agent's region of the mark: a rule's
// number is ALLOWED by that rule; the would-block value is WOULD_BLOCK;
// anything else was not evaluated (visibility mode, or a connection older
// than the ruleset) and is OBSERVED. The foreign bits are ignored.
func (s *Store) Classify(mark uint32) (innerwallv1.PolicyDecision, string) {
	index := RuleIndex(mark)
	if index == WouldBlockIndex {
		return innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK, ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id, ok := s.marks[index]; ok {
		return innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED, id
	}
	return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ""
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
