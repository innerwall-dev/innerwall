package compiler_test

import (
	"net/netip"
	"testing"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

const (
	tcp  = innerwallv1.Protocol_PROTOCOL_TCP
	udp  = innerwallv1.Protocol_PROTOCOL_UDP
	icmp = innerwallv1.Protocol_PROTOCOL_ICMP
)

func wl(t *testing.T, labels map[string]string, addrs ...string) registry.Workload {
	t.Helper()
	id, err := identity.NewWorkloadID()
	if err != nil {
		t.Fatal(err)
	}
	w := registry.Workload{ID: id, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY}
	for k, v := range labels {
		w.Labels = append(w.Labels, registry.Label{Key: k, Value: v})
	}
	for _, a := range addrs {
		w.Addresses = append(w.Addresses, netip.MustParseAddr(a))
	}
	return w
}

func findRule(p *innerwallv1.WorkloadPolicy, id string) *innerwallv1.ResolvedRule {
	for _, r := range p.GetInboundRules() {
		if r.GetRuleId() == id {
			return r
		}
	}
	return nil
}

func TestRenderResolvesSelectorsPeersAndServices(t *testing.T) {
	db := wl(t, map[string]string{"role": "db", "env": "prod"}, "10.0.0.10")
	web1 := wl(t, map[string]string{"role": "web", "env": "prod"}, "10.0.0.20", "fd00::20")
	web2 := wl(t, map[string]string{"role": "web", "env": "staging"}, "10.0.0.21")
	api := wl(t, map[string]string{"role": "api", "env": "prod"}, "10.0.0.30")
	noaddr := wl(t, map[string]string{"role": "web", "env": "prod"}) // matches but has no addresses yet
	web2.Mode = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED

	pg := policy.Service{ID: uuid.New(), Name: "postgres", Entries: []policy.ServiceEntry{
		{Protocol: tcp, Ports: []policy.PortRange{{Start: 5432, End: 5432}}},
		{Protocol: tcp, Ports: []policy.PortRange{{Start: 6000, End: 6010}}},
		{Protocol: udp}, // all udp ports
		{Protocol: icmp},
	}}
	corp := policy.AddressGroup{ID: uuid.New(), Name: "corp", CIDRs: []string{"192.168.0.0/16", "172.16.0.0/12"}}

	ruleA := uuid.New()
	ruleB := uuid.New()
	ruleDisabled := uuid.New()
	rulesets := []policy.Ruleset{
		{
			ID: uuid.New(), Name: "to-db", Enabled: true,
			Scope: policy.Selector{"role": {"db"}, "env": {"prod"}},
			Rules: []policy.Rule{
				{
					ID: ruleA, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
					Peers: []policy.Peer{
						{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web", "api"}, "env": {"prod"}}},
						{Kind: policy.PeerAddressGroup, AddressGroupID: corp.ID},
						{Kind: policy.PeerCIDR, CIDR: "203.0.113.7/32"},
					},
					ServiceIDs: []uuid.UUID{pg.ID},
					Entries:    []policy.ServiceEntry{{Protocol: tcp, Ports: []policy.PortRange{{Start: 22, End: 22}}}},
				},
				{
					ID: ruleDisabled, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: false,
					Peers:   []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "0.0.0.0/0"}},
					Entries: []policy.ServiceEntry{{Protocol: tcp}},
				},
			},
		},
		{
			ID: uuid.New(), Name: "web-ingress", Enabled: true,
			Scope: policy.Selector{"role": {"web"}},
			Rules: []policy.Rule{{
				ID: ruleB, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
				Peers:   []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "0.0.0.0/0"}},
				Entries: []policy.ServiceEntry{{Protocol: tcp, Ports: []policy.PortRange{{Start: 443, End: 443}, {Start: 80, End: 80}}}},
			}},
		},
		{
			ID: uuid.New(), Name: "disabled-set", Enabled: false,
			Scope: policy.Selector{"role": {"db"}},
			Rules: []policy.Rule{{ID: uuid.New(), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
				Peers: []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "0.0.0.0/0"}}, Entries: []policy.ServiceEntry{{Protocol: udp}}}},
		},
		{
			ID: uuid.New(), Name: "empty-scope-matches-nothing", Enabled: true,
			Scope: policy.Selector{},
			Rules: []policy.Rule{{ID: uuid.New(), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
				Peers: []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "0.0.0.0/0"}}, Entries: []policy.ServiceEntry{{Protocol: udp}}}},
		},
	}

	out := compiler.Render(&compiler.Inputs{
		Workloads:     []registry.Workload{db, web1, web2, api, noaddr},
		Rulesets:      rulesets,
		Services:      []policy.Service{pg},
		AddressGroups: []policy.AddressGroup{corp},
	})
	if len(out) != 5 {
		t.Fatalf("rendered %d workloads, want 5", len(out))
	}

	// The db workload: rule A expanded to tcp, udp, icmp; disabled rule and
	// disabled ruleset absent; empty-scope ruleset absent.
	dbp := out[db.ID]
	if n := len(dbp.GetInboundRules()); n != 3 {
		t.Fatalf("db rules = %v", dbp.GetInboundRules())
	}
	wantPeers := []string{"10.0.0.20/32", "10.0.0.30/32", "172.16.0.0/12", "192.168.0.0/16", "203.0.113.7/32", "fd00::20/128"}
	tcpRule := findRule(dbp, rendered.RuleID(ruleA.String(), tcp))
	if tcpRule == nil {
		t.Fatalf("no tcp rule for A: %v", dbp)
	}
	if got := tcpRule.GetPeerCidrs(); len(got) != len(wantPeers) {
		t.Fatalf("peers = %v, want %v", got, wantPeers)
	} else {
		for i := range wantPeers {
			if got[i] != wantPeers[i] {
				t.Fatalf("peers = %v, want %v", got, wantPeers)
			}
		}
	}
	// staging web2 (env mismatch: AND across keys) and api-in-staging never
	// appear; web1 appears via role OR.
	for _, p := range tcpRule.GetPeerCidrs() {
		if p == "10.0.0.21/32" {
			t.Fatal("staging workload leaked into a prod-only selector")
		}
	}
	// Ports: referenced service entries merged with the inline entry, sorted.
	wantPorts := [][2]uint32{{22, 22}, {5432, 5432}, {6000, 6010}}
	if len(tcpRule.GetPorts()) != len(wantPorts) {
		t.Fatalf("tcp ports = %v", tcpRule.GetPorts())
	}
	for i, w := range wantPorts {
		if tcpRule.GetPorts()[i].GetStart() != w[0] || tcpRule.GetPorts()[i].GetEnd() != w[1] {
			t.Fatalf("tcp ports = %v, want %v", tcpRule.GetPorts(), wantPorts)
		}
	}
	udpRule := findRule(dbp, rendered.RuleID(ruleA.String(), udp))
	if udpRule == nil || len(udpRule.GetPorts()) != 0 {
		t.Fatalf("udp rule = %v, want all ports", udpRule)
	}
	if len(udpRule.GetPeerCidrs()) != len(wantPeers) {
		t.Fatalf("udp peers = %v", udpRule.GetPeerCidrs())
	}
	icmpRule := findRule(dbp, rendered.RuleID(ruleA.String(), icmp))
	if icmpRule == nil || len(icmpRule.GetPorts()) != 0 || icmpRule.GetProtocol() != icmp {
		t.Fatalf("icmp rule = %v", icmpRule)
	}
	// Provenance: every rendered rule id splits back to its authored rule.
	for _, r := range dbp.GetInboundRules() {
		authored, p, err := rendered.Provenance(r.GetRuleId())
		if err != nil || authored != ruleA.String() || p != r.GetProtocol() {
			t.Fatalf("provenance of %q = %q %v %v", r.GetRuleId(), authored, p, err)
		}
	}

	// Web workloads: only rule B, both of them, regardless of env; the
	// workload without addresses still gets its own policy.
	for _, w := range []registry.Workload{web1, web2, noaddr} {
		p := out[w.ID]
		if len(p.GetInboundRules()) != 1 || p.GetInboundRules()[0].GetRuleId() != rendered.RuleID(ruleB.String(), tcp) {
			t.Fatalf("web policy = %v", p)
		}
		if got := p.GetInboundRules()[0].GetPorts(); got[0].GetStart() != 80 || got[1].GetStart() != 443 {
			t.Fatalf("web ports not sorted: %v", got)
		}
	}
	// Mode is carried from the workload.
	if out[web2.ID].GetMode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED || out[web1.ID].GetMode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY {
		t.Fatalf("modes = %v / %v", out[web2.ID].GetMode(), out[web1.ID].GetMode())
	}
	// api matches nothing: empty policy, but a policy nonetheless.
	if p := out[api.ID]; len(p.GetInboundRules()) != 0 {
		t.Fatalf("api policy = %v", p)
	}
	// Output is deterministic across renders.
	again := compiler.Render(&compiler.Inputs{Workloads: []registry.Workload{db, web1, web2, api, noaddr}, Rulesets: rulesets, Services: []policy.Service{pg}, AddressGroups: []policy.AddressGroup{corp}})
	for id := range out {
		a, _ := rendered.Marshal(out[id])
		b, _ := rendered.Marshal(again[id])
		if string(a) != string(b) {
			t.Fatalf("render of %v is not deterministic", id)
		}
	}
}

func TestRenderUsesAddressesCurrentAtRenderTime(t *testing.T) {
	db := wl(t, map[string]string{"role": "db"}, "10.0.0.10")
	web := wl(t, map[string]string{"role": "web"}, "10.0.0.20")
	rs := []policy.Ruleset{{ID: uuid.New(), Name: "x", Enabled: true, Scope: policy.Selector{"role": {"db"}},
		Rules: []policy.Rule{{ID: uuid.New(), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
			Peers:   []policy.Peer{{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web"}}}},
			Entries: []policy.ServiceEntry{{Protocol: tcp, Ports: []policy.PortRange{{Start: 5432, End: 5432}}}}}}}}
	first := compiler.Render(&compiler.Inputs{Workloads: []registry.Workload{db, web}, Rulesets: rs})
	web.Addresses = []netip.Addr{netip.MustParseAddr("10.0.0.99")}
	second := compiler.Render(&compiler.Inputs{Workloads: []registry.Workload{db, web}, Rulesets: rs})
	if p := first[db.ID].GetInboundRules()[0].GetPeerCidrs(); len(p) != 1 || p[0] != "10.0.0.20/32" {
		t.Fatalf("first peers = %v", p)
	}
	if p := second[db.ID].GetInboundRules()[0].GetPeerCidrs(); len(p) != 1 || p[0] != "10.0.0.99/32" {
		t.Fatalf("second peers = %v", p)
	}
	// And the delta between the two is exactly a peer swap.
	changes := rendered.Diff(first[db.ID], second[db.ID])
	if len(changes) != 2 || changes[0].GetAddPeers() == nil || changes[1].GetRemovePeers() == nil {
		t.Fatalf("delta = %v", changes)
	}
}

func TestPlanBumpsVersionsOnlyOnChange(t *testing.T) {
	a := wl(t, map[string]string{"role": "a"}, "10.0.0.1")
	b := wl(t, map[string]string{"role": "b"}, "10.0.0.2")
	c := wl(t, map[string]string{"role": "c"}, "10.0.0.3")
	rule := func(peer string) policy.Rule {
		return policy.Rule{ID: uuid.MustParse("33333333-3333-7333-8333-333333333333"), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
			Peers:   []policy.Peer{{Kind: policy.PeerCIDR, CIDR: peer}},
			Entries: []policy.ServiceEntry{{Protocol: tcp, Ports: []policy.PortRange{{Start: 22, End: 22}}}}}
	}
	in := &compiler.Inputs{Workloads: []registry.Workload{a, b, c}, Rulesets: []policy.Ruleset{
		{ID: uuid.New(), Name: "a", Enabled: true, Scope: policy.Selector{"role": {"a"}}, Rules: []policy.Rule{rule("10.1.0.0/16")}},
		{ID: uuid.New(), Name: "b", Enabled: true, Scope: policy.Selector{"role": {"b"}}, Rules: []policy.Rule{rule("10.2.0.0/16")}},
	}}

	// First render: nothing persisted, every workload gets version 1, even
	// c whose policy is empty: a snapshot must exist for every workload
	// from its first render on, so absence of a row counts as a change.
	outcomes := compiler.Plan(nil, compiler.Render(in))
	byID := map[identity.WorkloadID]compiler.Outcome{}
	for _, o := range outcomes {
		byID[o.ID] = o
	}
	for _, id := range []identity.WorkloadID{a.ID, b.ID, c.ID} {
		if !byID[id].Changed || byID[id].Policy.GetVersion() != 1 {
			t.Fatalf("first plan = %+v", outcomes)
		}
	}
	if len(byID[c.ID].Changes) != 0 {
		t.Fatalf("empty policy has changes: %+v", byID[c.ID])
	}

	// Persist what the first plan produced, re-render unchanged inputs:
	// empty diffs everywhere, no version moves.
	persisted := map[identity.WorkloadID]*innerwallv1.WorkloadPolicy{}
	for _, o := range outcomes {
		if o.Changed {
			persisted[o.ID] = o.Policy
		}
	}
	for _, o := range compiler.Plan(persisted, compiler.Render(in)) {
		if o.Changed || len(o.Changes) != 0 || o.Policy.GetVersion() != persisted[o.ID].GetVersion() {
			t.Fatalf("re-render of unchanged inputs changed %v: %+v", o.ID, o)
		}
	}

	// Change only b's rule: a keeps version 1, b moves to 2, c stays 1.
	in.Rulesets[1].Rules[0] = rule("10.9.0.0/16")
	for _, o := range compiler.Plan(persisted, compiler.Render(in)) {
		switch o.ID {
		case a.ID:
			if o.Changed || o.Policy.GetVersion() != 1 {
				t.Fatalf("a moved: %+v", o)
			}
		case b.ID:
			if !o.Changed || o.Policy.GetVersion() != 2 || len(o.Changes) != 2 {
				t.Fatalf("b = %+v", o)
			}
		case c.ID:
			if o.Changed || o.Policy.GetVersion() != 1 {
				t.Fatalf("c moved: %+v", o)
			}
		}
	}
}
