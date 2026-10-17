package nft

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current rendering")

const (
	ruleWeb = "0191e5c0-0000-7000-8000-000000000001/tcp"
	ruleDNS = "0191e5c0-0000-7000-8000-000000000002/udp"
	rulePng = "0191e5c0-0000-7000-8000-000000000003/icmp"
)

func multiRule(mode innerwallv1.EnforcementMode) *innerwallv1.WorkloadPolicy {
	return &innerwallv1.WorkloadPolicy{Version: 7, Mode: mode, InboundRules: []*innerwallv1.ResolvedRule{
		// Deliberately out of canonical order, with a duplicate peer and an
		// unmasked prefix: the rendering canonicalizes first.
		{RuleId: rulePng, Protocol: innerwallv1.Protocol_PROTOCOL_ICMP, PeerCidrs: []string{"10.0.0.0/8", "fd00::/64"}},
		{RuleId: ruleWeb, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: []string{"10.0.0.20/32", "10.0.0.20", "fd00::20/128", "192.168.1.7/24"}, Ports: []*innerwallv1.PortRange{{Start: 443, End: 443}, {Start: 8000, End: 8010}, {Start: 80, End: 80}}},
		{RuleId: ruleDNS, Protocol: innerwallv1.Protocol_PROTOCOL_UDP, PeerCidrs: []string{"10.0.0.0/24"}, Ports: []*innerwallv1.PortRange{{Start: 53, End: 53}}},
	}}
}

// render renders policy with a fresh allocation, which numbers rules in
// canonical order from 1: the numbering every golden file pins.
func render(t *testing.T, policy *innerwallv1.WorkloadPolicy, opts Options) string {
	t.Helper()
	marks, err := NewAllocator().Allocate(policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	script, err := Render(policy, marks, opts)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func goldenCases() map[string]*innerwallv1.WorkloadPolicy {
	return map[string]*innerwallv1.WorkloadPolicy{
		"enforced":       multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED),
		"simulation":     multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION),
		"visibility":     multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY),
		"empty-enforced": {Version: 1, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED},
		"no-peers-any-port": {Version: 2, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, InboundRules: []*innerwallv1.ResolvedRule{
			{RuleId: ruleWeb, Protocol: innerwallv1.Protocol_PROTOCOL_TCP},
		}},
	}
}

// TestRenderGolden pins the exact script each policy shape renders to.
// Run with -update to rewrite the files after a deliberate change.
func TestRenderGolden(t *testing.T) {
	for name, policy := range goldenCases() {
		t.Run(name, func(t *testing.T) {
			got := render(t, policy, Options{})
			path := filepath.Join("testdata", name+".nft")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil { //nolint:gosec // test fixture
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path) //nolint:gosec // test fixture
			if err != nil {
				t.Fatalf("reading golden file: %v (run with -update to create it)", err)
			}
			if got != string(want) {
				t.Fatalf("rendering differs from %s:\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
			}
		})
	}
}

// TestSimulationDiffersOnlyInTerminalRule is the structural statement of
// the simulation contract: the enforced and simulated renderings of one
// policy are identical line for line except the terminal rule, so a
// simulation verdict is evidence about exactly the ruleset enforcement
// would install.
func TestSimulationDiffersOnlyInTerminalRule(t *testing.T) {
	enforced := strings.Split(render(t, multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), Options{}), "\n")
	simulated := strings.Split(render(t, multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION), Options{}), "\n")
	if len(enforced) != len(simulated) {
		t.Fatalf("line counts differ: %d vs %d", len(enforced), len(simulated))
	}
	var differing []int
	for i := range enforced {
		if enforced[i] != simulated[i] {
			differing = append(differing, i)
		}
	}
	if len(differing) != 1 {
		t.Fatalf("differing lines = %v, want exactly one", differing)
	}
	e, s := enforced[differing[0]], simulated[differing[0]]
	if !strings.Contains(e, "drop") || !strings.Contains(e, "innerwall terminal enforced") {
		t.Fatalf("enforced terminal line = %q", e)
	}
	if !strings.Contains(s, "accept") || !strings.Contains(s, "ct mark set (ct mark & 0x0000ffff) | 0xffff0000") || !strings.Contains(s, "innerwall terminal simulation") {
		t.Fatalf("simulation terminal line = %q", s)
	}
	// Both log to the same group before their verdict.
	if !strings.Contains(e, "log prefix \"innerwall \" group 200") || !strings.Contains(s, "log prefix \"innerwall \" group 200") {
		t.Fatalf("terminal lines do not both log: %q / %q", e, s)
	}
}

// TestVisibilityObservesOnly is the structural statement of the
// visibility contract: the table holds one base chain on the input hook
// with a policy of accept and one rule, which references connection
// tracking, so the kernel tracks connections for the agent to observe,
// and accepts. Nothing in it drops, rejects, logs, marks, or reads a set,
// whatever rules the policy carries, and an unspecified mode renders the
// same table.
func TestVisibilityObservesOnly(t *testing.T) {
	script := render(t, multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY), Options{Table: "iwtest", NflogGroup: 7})
	want := "table inet iwtest {}\n" +
		"delete table inet iwtest\n" +
		"table inet iwtest {\n" +
		"\tchain " + ObserveChainName + " {\n" +
		"\t\ttype filter hook input priority " + Priority + "; policy accept;\n" +
		"\t\tct state new counter accept comment \"innerwall observation visibility\"\n" +
		"\t}\n" +
		"}\n"
	if script != want {
		t.Fatalf("visibility rendering:\n%s\nwant:\n%s", script, want)
	}
	for _, never := range []string{"drop", "reject", "log", "mark", "set ", "@", "policy drop"} {
		if strings.Contains(script, never) {
			t.Fatalf("visibility rendering contains %q:\n%s", never, script)
		}
	}
	unspecified := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED)
	if got := render(t, unspecified, Options{Table: "iwtest", NflogGroup: 7}); got != script {
		t.Fatalf("unspecified mode renders differently from visibility:\n%s", got)
	}
}

// TestRenderStructure checks the invariants every enforced rendering has:
// one transaction (declare, delete, recreate), loopback and established
// accepted before any rule, one set per rule and family, rules in
// canonical order carrying the rule id and mark in their comment, and the
// terminal rule last.
func TestRenderStructure(t *testing.T) {
	script := render(t, multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), Options{Table: "iwtest", NflogGroup: 7})
	lines := strings.Split(strings.TrimSpace(script), "\n")
	if lines[0] != "table inet iwtest {}" || lines[1] != "delete table inet iwtest" || lines[2] != "table inet iwtest {" {
		t.Fatalf("transaction prologue = %v", lines[:3])
	}
	if !strings.Contains(script, "hook input priority filter + 10; policy accept;") {
		t.Fatal("chain priority or policy missing")
	}
	order := []string{
		`iif "lo" accept`,
		"ct state established,related accept",
		"innerwall rule " + ruleWeb + " untracked",
		"innerwall rule " + ruleWeb + " mark 1",
		"innerwall rule " + ruleDNS + " untracked",
		"innerwall rule " + ruleDNS + " mark 2",
		"innerwall rule " + rulePng + " untracked",
		"innerwall rule " + rulePng + " mark 3",
		"log prefix \"innerwall \" group 7 drop",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(script, want)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order", want)
		}
		last = i
	}
	for _, id := range []string{ruleWeb, ruleDNS, rulePng} {
		for _, v6 := range []bool{false, true} {
			if !strings.Contains(script, "set "+SetName(id, v6)+" {") {
				t.Fatalf("set for %s v6=%v missing", id, v6)
			}
		}
	}
	// Canonicalized peers: the host route and the unmasked prefix.
	if !strings.Contains(script, "elements = { 10.0.0.20/32, 192.168.1.0/24 }") {
		t.Fatalf("web v4 elements not canonical:\n%s", script)
	}
	if !strings.Contains(script, "tcp dport { 80, 443, 8000-8010 }") {
		t.Fatalf("ports not canonical:\n%s", script)
	}
	if !strings.Contains(script, "meta l4proto icmp") || !strings.Contains(script, "meta l4proto ipv6-icmp") {
		t.Fatalf("icmp match missing:\n%s", script)
	}
	// Every mark write keeps the foreign bits and sets only the region.
	for _, want := range []string{
		"ct mark set (ct mark & 0x0000ffff) | 0x00010000 accept",
		"ct mark set (ct mark & 0x0000ffff) | 0x00020000 accept",
		"ct mark set (ct mark & 0x0000ffff) | 0x00030000 accept",
	} {
		if strings.Count(script, want) != 2 {
			t.Fatalf("%q should appear once per family:\n%s", want, script)
		}
	}
	if strings.Contains(script, "ct mark set 0x") || strings.Contains(script, "ct mark set 1") {
		t.Fatalf("an unmasked mark write:\n%s", script)
	}
}

// TestAdmitVerdictIndependentOfConnectionTracking holds the property
// issue #142 broke: a packet the kernel does not track (IPv6 neighbor
// discovery, or anything a host exempts with notrack) has no connection
// mark, a rule that reads one ends at the read, and an admitted packet
// must not fall through to the terminal rule because of it. Every
// marking accept is therefore preceded, directly, by an accept with the
// same match that admits untracked traffic and touches no connection
// state, and no other rule before the terminal one reads it. In both
// modes the terminal rule records before any connection step: the
// enforced one has none, and the simulated one logs and counts before
// its mark write, whose verdict on an untracked packet is the chain's
// policy of accept.
func TestAdmitVerdictIndependentOfConnectionTracking(t *testing.T) {
	for _, mode := range []innerwallv1.EnforcementMode{innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION} {
		t.Run(mode.String(), func(t *testing.T) {
			lines := strings.Split(render(t, multiRule(mode), Options{}), "\n")
			marking, untracked := 0, 0
			for i, line := range lines {
				line = strings.TrimSpace(line)
				switch {
				case strings.Contains(line, "ct state untracked"):
					untracked++
					if strings.Contains(line, "ct mark") || !strings.Contains(line, "ct state untracked accept comment") {
						t.Fatalf("untracked accept touches connection state or is not a plain accept: %q", line)
					}
				case strings.Contains(line, "ct mark set") && !strings.Contains(line, "innerwall terminal"):
					marking++
					prev := strings.TrimSpace(lines[i-1])
					match, _, _ := strings.Cut(line, " ct mark set ")
					if !strings.HasPrefix(prev, match+" ct state untracked accept ") {
						t.Fatalf("marking accept %q is not directly preceded by its untracked accept; got %q", line, prev)
					}
				}
			}
			if marking != 6 || untracked != 6 {
				t.Fatalf("marking accepts = %d, untracked accepts = %d, want one each per rule and family", marking, untracked)
			}
			terminal := strings.TrimSpace(lines[len(lines)-4])
			if mode == innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
				if !strings.HasPrefix(terminal, "log ") || strings.Contains(terminal, "ct ") || !strings.Contains(terminal, " drop comment") {
					t.Fatalf("enforced terminal rule = %q, want a log and a drop with no connection step", terminal)
				}
			} else {
				logAt, counterAt, ctAt := strings.Index(terminal, "log "), strings.Index(terminal, " counter "), strings.Index(terminal, " ct ")
				if logAt != 0 || counterAt < 0 || ctAt < counterAt || strings.Count(terminal, " ct ") != 1 {
					t.Fatalf("simulation terminal rule = %q, want log and counter before its one connection step", terminal)
				}
			}
		})
	}
}

// TestMarkWriteMask pins the mask every marking accept writes with: the
// foreign bits, 0x0000ffff, and never anything in the region, beside the
// rule's own value in the region (issue #143). `nft list` reads the
// kernel's (mask, xor) pair back as `ct mark & (mask | value) | value`,
// so an installed rule lists as `& 0x000Nffff | 0x000N0000`; the
// arithmetic part checks that the listed form writes exactly what the
// rendered one does, for every value and any prior mark, so no earlier
// region bits survive either way.
func TestMarkWriteMask(t *testing.T) {
	marks, err := NewAllocator().Allocate(multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), nil)
	if err != nil {
		t.Fatal(err)
	}
	script := render(t, multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), Options{})
	for id, index := range marks {
		want := fmt.Sprintf("ct mark set (ct mark & 0x0000ffff) | 0x%08x accept comment \"innerwall rule %s mark %d\"", RuleMark(index), id, index)
		if strings.Count(script, want) != 2 {
			t.Fatalf("rule %s: want %q once per family:\n%s", id, want, script)
		}
	}
	if strings.Count(script, "ct mark & 0x0000ffff") != 6 || strings.Count(script, "ct mark &") != 6 {
		t.Fatalf("a mark write masks with something other than the foreign bits:\n%s", script)
	}
	priors := []uint32{0, 0x2a, 0xffff, 0x00010000, 0x00040000, 0x0005002a, 0xfffe0000, 0xffffffff, 0xdeadbeef}
	for index := uint32(1); index <= WouldBlockIndex; index++ {
		v := index << MarkShift
		for _, x := range priors {
			rendered, listed := (x&ForeignMask)|v, (x&(ForeignMask|v))|v
			if rendered != listed || RuleIndex(rendered) != index || rendered&ForeignMask != x&ForeignMask {
				t.Fatalf("value %#x over prior %#x: rendered %#x, listed %#x", v, x, rendered, listed)
			}
		}
	}
}

// TestMarkRegion checks the region arithmetic: a rule's mark occupies
// bits 16 through 31, the foreign bits are never part of a mark the agent
// writes, and reading a mark ignores whatever the foreign bits hold.
func TestMarkRegion(t *testing.T) {
	if RuleMark(1) != 0x00010000 || RuleMark(0xfffe) != 0xfffe0000 || WouldBlockMark != 0xffff0000 {
		t.Fatalf("RuleMark(1)=%#x RuleMark(max)=%#x WouldBlockMark=%#x", RuleMark(1), RuleMark(0xfffe), WouldBlockMark)
	}
	if RuleMark(1)&ForeignMask != 0 || WouldBlockMark&ForeignMask != 0 {
		t.Fatal("a mark the agent writes touches the foreign bits")
	}
	if RuleIndex(RuleMark(7)|0x2a) != 7 || RuleIndex(WouldBlockMark|0xbeef) != WouldBlockIndex || RuleIndex(0x2a) != 0 || RuleIndex(0) != 0 {
		t.Fatal("RuleIndex does not ignore the foreign bits")
	}
	if MaxRules != 0xfffe {
		t.Fatalf("MaxRules = %d", MaxRules)
	}
}

func TestSetNameAndMarks(t *testing.T) {
	if got := SetName(ruleWeb, false); got != "r_0191e5c0_0000_7000_8000_000000000001_tcp_v4" {
		t.Fatalf("SetName = %s", got)
	}
	if got := SetName("a-b/udp", true); got != "r_a_b_udp_v6" {
		t.Fatalf("SetName = %s", got)
	}
	marks, err := NewAllocator().Allocate(multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 3 || marks[ruleWeb] != 1 || marks[ruleDNS] != 2 || marks[rulePng] != 3 {
		t.Fatalf("fresh allocation = %v", marks)
	}
	// A rule the allocation gives no value is refused, never rendered
	// with a made-up one.
	delete(marks, ruleDNS)
	if _, err := Render(multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), marks, Options{}); !errors.Is(err, ErrUnallocatedRule) {
		t.Fatalf("Render without a value for every rule: %v", err)
	}
}

// TestRenderDelta checks that peer-only differences become set element
// operations in one script and that anything else falls back to a full
// rendering.
func TestRenderDelta(t *testing.T) {
	from := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	to := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	to.Version = 8
	to.InboundRules[1].PeerCidrs = []string{"10.0.0.21/32", "fd00::20/128", "fd00::21/128", "192.168.1.0/24"}
	script, ok := RenderDelta(from, to, Options{})
	if !ok {
		t.Fatal("peer-only change was not a delta")
	}
	want := "add element inet innerwall " + SetName(ruleWeb, false) + " { 10.0.0.21/32 }\n" +
		"add element inet innerwall " + SetName(ruleWeb, true) + " { fd00::21/128 }\n" +
		"delete element inet innerwall " + SetName(ruleWeb, false) + " { 10.0.0.20/32 }\n"
	if script != want {
		t.Fatalf("delta script:\n%s\nwant:\n%s", script, want)
	}

	if _, ok := RenderDelta(from, from, Options{}); ok {
		t.Fatal("no change rendered a delta")
	}
	mode := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION)
	if _, ok := RenderDelta(from, mode, Options{}); ok {
		t.Fatal("a mode change rendered a delta")
	}
	portsChanged := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	portsChanged.InboundRules[2].Ports = []*innerwallv1.PortRange{{Start: 5353, End: 5353}}
	if _, ok := RenderDelta(from, portsChanged, Options{}); ok {
		t.Fatal("a port change rendered a delta")
	}
	removed := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	removed.InboundRules = removed.InboundRules[:2]
	if _, ok := RenderDelta(from, removed, Options{}); ok {
		t.Fatal("a rule removal rendered a delta")
	}
	// A visibility table carries no sets, so a peer-only change there is
	// a full rendering, never a set update the kernel would refuse
	// (issue #45). Simulation carries the same sets as enforcement.
	visFrom := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY)
	visTo := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY)
	visTo.Version = 8
	visTo.InboundRules[1].PeerCidrs = to.InboundRules[1].PeerCidrs
	if script, ok := RenderDelta(visFrom, visTo, Options{}); ok {
		t.Fatalf("a peer-only change on a visibility table rendered a set update:\n%s", script)
	}
	unspecified := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED)
	unspecifiedTo := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED)
	unspecifiedTo.InboundRules[1].PeerCidrs = to.InboundRules[1].PeerCidrs
	if _, ok := RenderDelta(unspecified, unspecifiedTo, Options{}); ok {
		t.Fatal("a peer-only change on an unspecified-mode table rendered a set update")
	}
	simFrom := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION)
	simTo := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION)
	simTo.InboundRules[1].PeerCidrs = to.InboundRules[1].PeerCidrs
	if _, ok := RenderDelta(simFrom, simTo, Options{}); !ok {
		t.Fatal("a peer-only change on a simulation table was not a delta")
	}

	// Peers changed on one rule and ports on another: the whole thing is
	// a full rendering.
	both := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	both.InboundRules[1].PeerCidrs = append(both.InboundRules[1].PeerCidrs, "10.0.0.22/32")
	both.InboundRules[2].Ports = nil
	if _, ok := RenderDelta(from, both, Options{}); ok {
		t.Fatal("a mixed change rendered a delta")
	}
}
