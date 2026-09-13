package nft

import (
	"flag"
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
			got := Render(policy, Options{})
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
	enforced := strings.Split(Render(multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), Options{}), "\n")
	simulated := strings.Split(Render(multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION), Options{}), "\n")
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

// TestRenderStructure checks the invariants every enforced rendering has:
// one transaction (declare, delete, recreate), loopback and established
// accepted before any rule, one set per rule and family, rules in
// canonical order carrying the rule id and mark in their comment, and the
// terminal rule last.
func TestRenderStructure(t *testing.T) {
	script := Render(multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED), Options{Table: "iwtest", NflogGroup: 7})
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
		"innerwall rule " + ruleWeb + " mark 1",
		"innerwall rule " + ruleDNS + " mark 2",
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
	marks := Marks(multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED))
	if len(marks) != 3 || marks[1] != ruleWeb || marks[2] != ruleDNS || marks[3] != rulePng {
		t.Fatalf("marks = %v", marks)
	}
	if _, ok := marks[WouldBlockMark]; ok {
		t.Fatal("the would-block mark collides with a rule mark")
	}
	if len(Marks(nil)) != 0 {
		t.Fatal("nil policy has marks")
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
	// Peers changed on one rule and ports on another: the whole thing is
	// a full rendering.
	both := multiRule(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED)
	both.InboundRules[1].PeerCidrs = append(both.InboundRules[1].PeerCidrs, "10.0.0.22/32")
	both.InboundRules[2].Ports = nil
	if _, ok := RenderDelta(from, both, Options{}); ok {
		t.Fatal("a mixed change rendered a delta")
	}
}
