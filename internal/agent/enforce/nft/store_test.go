package nft

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// fakeRunner records scripts and can refuse them.
type fakeRunner struct {
	scripts []string
	refuse  func(script string) error
}

func (f *fakeRunner) Apply(_ context.Context, script string) error {
	if f.refuse != nil {
		if err := f.refuse(script); err != nil {
			return err
		}
	}
	f.scripts = append(f.scripts, script)
	return nil
}

func (f *fakeRunner) List(context.Context, string) (string, error) { return "", nil }

func isDelta(script string) bool {
	return strings.HasPrefix(script, "add element") || strings.HasPrefix(script, "delete element")
}

// TestStoreAppliesPersistsAndRestores runs the store's lifecycle against
// a recording runner: a first apply is a full rendering and is persisted
// with mode 0600; a peer-only change is a set update; a refused set
// update falls back to a full rendering; a refused full rendering leaves
// the installed policy untouched; a new store loads the persisted policy,
// reports its version before any connection, and restores it as a full
// rendering; the mark table follows the installed policy.
func TestStoreAppliesPersistsAndRestores(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runner := &fakeRunner{}
	s := New(Config{Runner: runner, StateDir: dir})
	if s.Current() != nil || s.Mode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY || s.LastApply() != ApplyNone {
		t.Fatal("fresh store is not empty")
	}
	if err := s.Load(); err != nil {
		t.Fatalf("Load with no file: %v", err)
	}

	v7 := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	if err := s.Apply(ctx, v7); err != nil {
		t.Fatal(err)
	}
	if s.LastApply() != ApplyFull || len(runner.scripts) != 1 || !strings.HasPrefix(runner.scripts[0], "table inet innerwall {}\n") {
		t.Fatalf("first apply: kind=%s scripts=%d", s.LastApply(), len(runner.scripts))
	}
	if s.Current().GetVersion() != 7 || s.Mode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
		t.Fatalf("current = %v", s.Current())
	}
	info, err := os.Stat(filepath.Join(dir, enforce.PolicyFileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("policy file stat = %v, %v", info, err)
	}
	if d, id := s.Classify(1); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != ruleWeb {
		t.Fatalf("Classify(1) = %v %s", d, id)
	}
	if d, _ := s.Classify(WouldBlockMark); d != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
		t.Fatalf("Classify(would-block) = %v", d)
	}
	if d, _ := s.Classify(0); d != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatalf("Classify(0) = %v", d)
	}
	if s.TerminalDecision() != innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED {
		t.Fatalf("terminal decision = %v", s.TerminalDecision())
	}

	// Peer-only change: a set update.
	v8 := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	v8.Version = 8
	v8.InboundRules[1].PeerCidrs = append(v8.InboundRules[1].PeerCidrs, "10.0.0.21/32")
	if err := s.Apply(ctx, v8); err != nil {
		t.Fatal(err)
	}
	if s.LastApply() != ApplyDelta || !isDelta(runner.scripts[1]) || s.Current().GetVersion() != 8 {
		t.Fatalf("delta apply: kind=%s script=%q", s.LastApply(), runner.scripts[1])
	}

	// A refused set update falls back to a full rendering.
	v9 := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	v9.Version = 9
	v9.InboundRules[1].PeerCidrs = append(v9.InboundRules[1].PeerCidrs, "10.0.0.21/32", "10.0.0.22/32")
	runner.refuse = func(script string) error {
		if isDelta(script) {
			return errors.New("refused")
		}
		return nil
	}
	if err := s.Apply(ctx, v9); err != nil {
		t.Fatal(err)
	}
	if s.LastApply() != ApplyFull || s.Current().GetVersion() != 9 {
		t.Fatalf("fallback apply: kind=%s version=%d", s.LastApply(), s.Current().GetVersion())
	}

	// A refused full rendering changes nothing: not the installed policy,
	// not the persisted file, not the marks.
	sim := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION)
	sim.Version = 10
	sim.InboundRules = sim.InboundRules[:1]
	runner.refuse = func(string) error { return errors.New("kernel said no") }
	if err := s.Apply(ctx, sim); err == nil || !strings.Contains(err.Error(), "kernel said no") {
		t.Fatalf("refused apply err = %v", err)
	}
	if s.Current().GetVersion() != 9 || s.Mode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
		t.Fatalf("refused apply changed the installed policy: %v", s.Current())
	}
	if d, id := s.Classify(3); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != rulePng {
		t.Fatalf("marks changed on a refused apply: %v %s", d, id)
	}
	runner.refuse = nil

	// A new store over the same directory: the persisted policy is loaded
	// (its version is what Hello would report) and restored in full.
	runner2 := &fakeRunner{}
	s2 := New(Config{Runner: runner2, StateDir: dir})
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if !rendered.Equal(s2.Current(), v9) || s2.Current().GetVersion() != 9 {
		t.Fatalf("loaded = %v", s2.Current())
	}
	if len(runner2.scripts) != 0 {
		t.Fatal("Load touched the kernel")
	}
	if err := s2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if s2.LastApply() != ApplyFull || len(runner2.scripts) != 1 || runner2.scripts[0] != Render(v9, Options{}) {
		t.Fatalf("restore: kind=%s scripts=%d", s2.LastApply(), len(runner2.scripts))
	}
	if d, id := s2.Classify(2); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != ruleDNS {
		t.Fatalf("marks after restore: %v %s", d, id)
	}

	// Simulation mode changes the terminal decision and is a full apply.
	if err := s2.Apply(ctx, sim); err != nil {
		t.Fatal(err)
	}
	if s2.LastApply() != ApplyFull || s2.TerminalDecision() != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
		t.Fatalf("simulation apply: kind=%s terminal=%v", s2.LastApply(), s2.TerminalDecision())
	}
	if d, _ := s2.Classify(2); d != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatalf("stale mark still classified: %v", d)
	}

	// Teardown is the declare-and-delete of the owned table and nothing
	// else; the persisted policy stays.
	runner3 := &fakeRunner{}
	if err := Teardown(ctx, runner3, Options{}); err != nil {
		t.Fatal(err)
	}
	if runner3.scripts[0] != "table inet innerwall {}\ndelete table inet innerwall\n" {
		t.Fatalf("teardown script = %q", runner3.scripts[0])
	}
	if _, err := os.Stat(filepath.Join(dir, enforce.PolicyFileName)); err != nil {
		t.Fatalf("teardown removed the persisted policy: %v", err)
	}
}

// TestStoreRefusesCorruptPolicyFile checks that a corrupt persisted
// policy loads nothing and applies nothing: the store stays empty and
// Restore is a no-op, so the kernel keeps whatever it holds.
func TestStoreRefusesCorruptPolicyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, enforce.PolicyFileName), []byte("innerwall-policy-v1\ngarbage that is long enough to carry a checksum trailer........"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	s := New(Config{Runner: runner, StateDir: dir})
	err := s.Load()
	if !errors.Is(err, enforce.ErrCorruptPolicyFile) {
		t.Fatalf("Load err = %v", err)
	}
	if s.Current() != nil {
		t.Fatal("corrupt file loaded a policy")
	}
	if err := s.Restore(context.Background()); err != nil || len(runner.scripts) != 0 {
		t.Fatalf("Restore after corrupt load: err=%v scripts=%d", err, len(runner.scripts))
	}
}
