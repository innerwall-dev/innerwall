package compiler_test

import (
	"testing"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

func rule(id string, peers ...string) *innerwallv1.ResolvedRule {
	return &innerwallv1.ResolvedRule{RuleId: id, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: peers, Ports: []*innerwallv1.PortRange{{Start: 80, End: 80}}}
}

func TestDryRunClassifiesTheDelta(t *testing.T) {
	a, _ := identity.NewWorkloadID()
	b, _ := identity.NewWorkloadID()
	c, _ := identity.NewWorkloadID()
	previous := map[identity.WorkloadID]*innerwallv1.WorkloadPolicy{
		a: {Version: 3, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, InboundRules: []*innerwallv1.ResolvedRule{rule("r1/tcp", "10.0.0.1/32"), rule("r2/tcp", "10.0.0.2/32"), rule("r3/tcp", "10.0.0.3/32")}},
		b: {Version: 1, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY},
	}
	current := map[identity.WorkloadID]*innerwallv1.WorkloadPolicy{
		// r1 gains a peer (changed), r2 is gone (removed), r3 is
		// untouched, r4 is new (added), and the mode moves.
		a: {Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, InboundRules: []*innerwallv1.ResolvedRule{rule("r1/tcp", "10.0.0.1/32", "10.0.0.9/32"), rule("r3/tcp", "10.0.0.3/32"), rule("r4/tcp", "10.0.0.4/32")}},
		// b is unchanged and not reported.
		b: {Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY},
		// c has no persisted policy: everything is added against the
		// empty policy at version 0.
		c: {Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, InboundRules: []*innerwallv1.ResolvedRule{rule("r5/tcp", "10.0.0.5/32")}},
	}
	diffs := compiler.DryRun(previous, current)
	if len(diffs) != 2 {
		t.Fatalf("diffs = %+v, want a and c", diffs)
	}
	byID := map[identity.WorkloadID]compiler.WorkloadDiff{}
	for _, d := range diffs {
		byID[d.ID] = d
	}
	da := byID[a]
	if da.Version != 3 || da.Mode == nil || da.Mode.From != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION || da.Mode.To != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
		t.Fatalf("a = %+v", da)
	}
	if len(da.Added) != 1 || da.Added[0].GetRuleId() != "r4/tcp" || len(da.Removed) != 1 || da.Removed[0].GetRuleId() != "r2/tcp" {
		t.Fatalf("a added %v removed %v", da.Added, da.Removed)
	}
	if len(da.Changed) != 1 || da.Changed[0].Before.GetRuleId() != "r1/tcp" || len(da.Changed[0].After.GetPeerCidrs()) != 2 {
		t.Fatalf("a changed %v", da.Changed)
	}
	dc := byID[c]
	if dc.Version != 0 || dc.Mode != nil || len(dc.Added) != 1 || len(dc.Removed) != 0 || len(dc.Changed) != 0 {
		t.Fatalf("c = %+v", dc)
	}
	if again := compiler.DryRun(previous, current); len(again) != 2 || again[0].ID != diffs[0].ID {
		t.Fatal("a repeated dry run differs")
	}
}
