package nft

import (
	"context"
	"errors"
	"fmt"
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
// the installed policy untouched with the persisted file one version
// ahead, the desired state; a new store loads the persisted policy,
// reports its version before any connection, and restores it as a full
// rendering; marks resolve through the persisted allocation.
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
	if d, id := s.Classify(RuleMark(1)); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != ruleWeb {
		t.Fatalf("Classify(rule 1) = %v %s", d, id)
	}
	// Foreign bits in the mark change nothing about the classification.
	if d, id := s.Classify(RuleMark(1) | 0x2a); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != ruleWeb {
		t.Fatalf("Classify(rule 1 with foreign bits) = %v %s", d, id)
	}
	if d, _ := s.Classify(WouldBlockMark); d != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
		t.Fatalf("Classify(would-block) = %v", d)
	}
	if d, _ := s.Classify(WouldBlockMark | 0xbeef); d != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
		t.Fatalf("Classify(would-block with foreign bits) = %v", d)
	}
	if d, _ := s.Classify(0); d != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatalf("Classify(0) = %v", d)
	}
	// A foreign mark alone, and a raw rule number outside the region, are
	// not ours.
	if d, _ := s.Classify(0x2a); d != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatalf("Classify(foreign only) = %v", d)
	}
	if d, _ := s.Classify(1); d != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatalf("Classify(1) = %v; the raw number is not in the region", d)
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

	// A refused full rendering leaves the installed policy and its marks
	// as they were. The policy was persisted before the kernel saw it, so
	// the file is one version ahead: the desired state, which a restart
	// restores and the control plane's recovery snapshot re-applies.
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
	if d, id := s.Classify(RuleMark(3)); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != rulePng {
		t.Fatalf("marks changed on a refused apply: %v %s", d, id)
	}
	if onDisk, err := (enforce.PolicyFile{Path: enforce.PolicyPath(dir)}).Load(); err != nil || !rendered.Equal(onDisk, sim) {
		t.Fatalf("persisted policy after a refused kernel transaction = %v, %v; want version 10, persisted first", onDisk, err)
	}
	runner.refuse = nil

	// A new store over the same directory: the persisted policy is loaded
	// (its version is what Hello would report) and restored in full, with
	// every rule on the value the allocation gave it.
	runner2 := &fakeRunner{}
	s2 := New(Config{Runner: runner2, StateDir: dir})
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if !rendered.Equal(s2.Current(), sim) || s2.Current().GetVersion() != 10 {
		t.Fatalf("loaded = %v", s2.Current())
	}
	if len(runner2.scripts) != 0 {
		t.Fatal("Load touched the kernel")
	}
	if err := s2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if s2.LastApply() != ApplyFull || len(runner2.scripts) != 1 || !strings.Contains(runner2.scripts[0], "innerwall rule "+rulePng+" mark 3\"") || !strings.Contains(runner2.scripts[0], "innerwall terminal simulation") {
		t.Fatalf("restore: kind=%s scripts=%q", s2.LastApply(), runner2.scripts)
	}
	if s2.TerminalDecision() != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
		t.Fatalf("restored terminal decision = %v", s2.TerminalDecision())
	}
	// The values survive the restart, including the removed rules': a
	// connection admitted by the web or DNS rule before the restart still
	// names that rule.
	if d, id := s2.Classify(RuleMark(2)); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != ruleDNS {
		t.Fatalf("removed rule's mark after restore: %v %s", d, id)
	}
	if d, id := s2.Classify(RuleMark(3)); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != rulePng {
		t.Fatalf("installed rule's mark after restore: %v %s", d, id)
	}
	// A value never handed out names nothing.
	if d, _ := s2.Classify(RuleMark(4)); d != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatalf("unallocated mark classified: %v", d)
	}

	// More rules than the region can number is refused before the kernel
	// sees anything.
	huge := &innerwallv1.WorkloadPolicy{Version: 11, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED}
	for i := 0; i <= MaxRules; i++ {
		huge.InboundRules = append(huge.InboundRules, &innerwallv1.ResolvedRule{RuleId: fmt.Sprintf("%05x/tcp", i), Protocol: innerwallv1.Protocol_PROTOCOL_TCP})
	}
	before := len(runner2.scripts)
	if err := s2.Apply(ctx, huge); !errors.Is(err, ErrTooManyRules) {
		t.Fatalf("oversized policy err = %v", err)
	}
	if len(runner2.scripts) != before || s2.Current().GetVersion() != 10 {
		t.Fatal("oversized policy reached the kernel or replaced the installed policy")
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

// TestApplyFailsWithoutPersistence is the regression for an apply that
// was acknowledged with no persisted copy: when the policy cannot be
// persisted, the apply fails, the kernel is never touched, and the store,
// the file, and the last acknowledgement still agree. The state directory
// here is a regular file, so persistence fails deterministically.
func TestApplyFailsWithoutPersistence(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "state-file")
	if err := os.WriteFile(dir, []byte("blocks the state directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	s := New(Config{Runner: runner, StateDir: dir})
	p := &innerwallv1.WorkloadPolicy{Version: 7, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED}
	if err := s.Apply(ctx, p); err == nil {
		t.Fatal("Apply succeeded with nowhere to persist the policy")
	}
	if len(runner.scripts) != 0 {
		t.Fatalf("the kernel was touched by an apply that could not persist: %q", runner.scripts)
	}
	if s.Current() != nil || s.LastApply() != ApplyNone {
		t.Fatalf("store changed by a failed apply: current=%v last=%s", s.Current(), s.LastApply())
	}
}

// TestPersistFailureLeavesTheLastAcknowledgedState covers the two
// persistence steps on a store that already holds an acknowledged
// policy: whether the allocation or the policy file fails to persist,
// the apply fails, no script reaches the kernel, the installed policy is
// still the acknowledged one, and a restart restores that version.
func TestPersistFailureLeavesTheLastAcknowledgedState(t *testing.T) {
	ctx := context.Background()
	v7 := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	v8 := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION)
	v8.Version = 8
	for _, blocked := range []string{MarksFileName, enforce.PolicyFileName} {
		t.Run(blocked, func(t *testing.T) {
			dir := t.TempDir()
			runner := &fakeRunner{}
			s := New(Config{Runner: runner, StateDir: dir})
			if err := s.Apply(ctx, v7); err != nil {
				t.Fatal(err)
			}
			// A non-empty directory where the file goes: the write's
			// final rename cannot replace it.
			path := filepath.Join(dir, blocked)
			saved, err := os.ReadFile(path) //nolint:gosec // test fixture
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(path, "occupied"), 0o700); err != nil {
				t.Fatal(err)
			}
			before := len(runner.scripts)
			if err := s.Apply(ctx, v8); err == nil {
				t.Fatal("Apply succeeded without persisting")
			}
			if len(runner.scripts) != before {
				t.Fatalf("the kernel was touched: %q", runner.scripts[before:])
			}
			if !rendered.Equal(s.Current(), v7) || s.Mode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
				t.Fatalf("installed policy after a failed persist = %v", s.Current())
			}
			// Put the file back as it was and restart: the acknowledged
			// version comes back.
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, saved, 0o600); err != nil {
				t.Fatal(err)
			}
			restarted := New(Config{Runner: &fakeRunner{}, StateDir: dir})
			if err := restarted.Load(); err != nil {
				t.Fatal(err)
			}
			if !rendered.Equal(restarted.Current(), v7) {
				t.Fatalf("restart after a failed save of %s loaded %v, want version 7", blocked, restarted.Current())
			}
		})
	}
}

// persistChecker is a runner that, at the moment a script reaches it,
// reads the state directory and records what was persisted: the
// observable form of persist-before-apply.
type persistChecker struct {
	fakeRunner
	dir       string
	persisted []uint64
}

func (p *persistChecker) Apply(ctx context.Context, script string) error {
	onDisk, err := (enforce.PolicyFile{Path: enforce.PolicyPath(p.dir)}).Load()
	if err != nil {
		return err
	}
	p.persisted = append(p.persisted, onDisk.GetVersion())
	return p.fakeRunner.Apply(ctx, script)
}

// TestApplyPersistsBeforeTheKernel checks the ordering itself: whenever
// a script reaches the kernel, full or set update, the policy it installs
// is already on disk.
func TestApplyPersistsBeforeTheKernel(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runner := &persistChecker{dir: dir}
	s := New(Config{Runner: runner, StateDir: dir})
	v7 := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	v8 := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	v8.Version = 8
	v8.InboundRules[1].PeerCidrs = append(v8.InboundRules[1].PeerCidrs, "10.0.0.21/32")
	for _, p := range []*innerwallv1.WorkloadPolicy{v7, v8} {
		if err := s.Apply(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if s.LastApply() != ApplyDelta {
		t.Fatalf("second apply kind = %s, want a set update", s.LastApply())
	}
	if len(runner.persisted) != 2 || runner.persisted[0] != 7 || runner.persisted[1] != 8 {
		t.Fatalf("versions on disk as each script reached the kernel = %v, want [7 8]", runner.persisted)
	}
}

// TestMarksStableAcrossVersions is the regression for attribution that
// followed the installed policy's order: a connection the kernel marked
// under one version must name the same rule after a version adds a rule
// that sorts before it, and after a version removes its rule it names the
// removed rule, never the rule that came to sort into its position.
func TestMarksStableAcrossVersions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := New(Config{Runner: &fakeRunner{}, StateDir: dir})
	ruleB := &innerwallv1.ResolvedRule{RuleId: "b/tcp", Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: []string{"10.0.0.1/32"}}
	ruleA := &innerwallv1.ResolvedRule{RuleId: "a/tcp", Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: []string{"10.1.0.1/32"}}
	ruleC := &innerwallv1.ResolvedRule{RuleId: "c/tcp", Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: []string{"10.2.0.1/32"}}
	at := func(version uint64, rules ...*innerwallv1.ResolvedRule) *innerwallv1.WorkloadPolicy {
		return &innerwallv1.WorkloadPolicy{Version: version, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, InboundRules: rules}
	}
	classify := func(s *Store, mark uint32) string {
		t.Helper()
		d, id := s.Classify(mark)
		if d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED {
			t.Fatalf("Classify(%#x) = %v, want ALLOWED", mark, d)
		}
		return id
	}

	if err := s.Apply(ctx, at(1, ruleB)); err != nil {
		t.Fatal(err)
	}
	markB, ok := s.Mark("b/tcp")
	if !ok || classify(s, markB) != "b/tcp" {
		t.Fatalf("b/tcp mark = %#x, %v", markB, ok)
	}

	// Version 2 adds a rule that sorts first. b/tcp keeps its value, and
	// the connection marked under version 1 still names it.
	if err := s.Apply(ctx, at(2, ruleB, ruleA)); err != nil {
		t.Fatal(err)
	}
	if got := classify(s, markB); got != "b/tcp" {
		t.Fatalf("connection admitted by b/tcp under version 1 attributed to %s after version 2", got)
	}
	markA, _ := s.Mark("a/tcp")
	if markA == markB {
		t.Fatal("the new rule took the existing rule's value")
	}

	// Version 3 removes b/tcp and adds c/tcp. The old connection names
	// the removed rule; the new rule takes a value never handed out.
	if err := s.Apply(ctx, at(3, ruleA, ruleC)); err != nil {
		t.Fatal(err)
	}
	if got := classify(s, markB); got != "b/tcp" {
		t.Fatalf("connection admitted by the removed b/tcp attributed to %s", got)
	}
	markC, _ := s.Mark("c/tcp")
	if markC == markA || markC == markB {
		t.Fatalf("c/tcp took a value in use or freed: %#x", markC)
	}
	if !strings.Contains(s.mustScript(t), fmt.Sprintf("innerwall rule c/tcp mark %d\"", RuleIndex(markC))) {
		t.Fatal("rendering does not write c/tcp's allocated value")
	}

	// The allocation survives a restart with every value, removed rules'
	// included, and b/tcp coming back gets its own value again.
	restarted := New(Config{Runner: &fakeRunner{}, StateDir: dir})
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]uint32{"a/tcp": markA, "b/tcp": markB, "c/tcp": markC} {
		if got := classify(restarted, want); got != id {
			t.Fatalf("after restart %#x names %s, want %s", want, got, id)
		}
	}
	if err := restarted.Apply(ctx, at(4, ruleA, ruleB, ruleC)); err != nil {
		t.Fatal(err)
	}
	if got, _ := restarted.Mark("b/tcp"); got != markB {
		t.Fatalf("b/tcp returned with %#x, want its own %#x", got, markB)
	}
}

// mustScript returns the installed policy rendered with the store's
// allocation.
func (s *Store) mustScript(t *testing.T) string {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	marks, err := s.alloc.Clone().Allocate(s.current, s.current)
	if err != nil {
		t.Fatal(err)
	}
	script, err := Render(s.current, marks, s.opts)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

// TestLoadSeedsAMissingOrCorruptAllocation checks the two ways a policy
// file can be found without a usable allocation beside it. Missing, as a
// state directory written before the allocation was persisted, it is
// seeded in canonical order: exactly the numbering such a directory's
// kernel ruleset carries. Corrupt, it is seeded the same way and the
// policy is still restored.
func TestLoadSeedsAMissingOrCorruptAllocation(t *testing.T) {
	ctx := context.Background()
	for name, damage := range map[string]func(path string) error{
		"missing": os.Remove,
		"corrupt": func(path string) error {
			return os.WriteFile(path, []byte("innerwall-marks-v1\nnot the allocation at all, and long enough for a checksum....."), 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s := New(Config{Runner: &fakeRunner{}, StateDir: dir})
			if err := s.Apply(ctx, multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)); err != nil {
				t.Fatal(err)
			}
			if err := damage(MarksPath(dir)); err != nil {
				t.Fatal(err)
			}
			runner := &fakeRunner{}
			restarted := New(Config{Runner: runner, StateDir: dir})
			if err := restarted.Load(); err != nil {
				t.Fatal(err)
			}
			if err := restarted.Restore(ctx); err != nil {
				t.Fatal(err)
			}
			if len(runner.scripts) != 1 || runner.scripts[0] != render(t, multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), Options{}) {
				t.Fatalf("restored script = %q", runner.scripts)
			}
			for i, id := range []string{ruleWeb, ruleDNS, rulePng} {
				if d, got := restarted.Classify(RuleMark(uint32(i + 1))); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || got != id { //nolint:gosec // three rules
					t.Fatalf("seeded value %d = %v %s, want %s", i+1, d, got, id)
				}
			}
		})
	}
}
