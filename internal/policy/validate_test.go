package policy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/policy"
)

var (
	svcID   = uuid.MustParse("11111111-1111-7111-8111-111111111111")
	groupID = uuid.MustParse("22222222-2222-7222-8222-222222222222")
	refs    = policy.References{
		Services:      map[uuid.UUID]struct{}{svcID: {}},
		AddressGroups: map[uuid.UUID]struct{}{groupID: {}},
	}
)

func goodRuleset() *policy.Ruleset {
	return &policy.Ruleset{
		Name:  "web-to-db",
		Scope: policy.Selector{"role": {"db"}},
		Rules: []policy.Rule{{
			Direction:  innerwallv1.Direction_DIRECTION_INBOUND,
			Enabled:    true,
			Peers:      []policy.Peer{{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web", "api"}}}},
			ServiceIDs: []uuid.UUID{svcID},
		}},
	}
}

func TestValidateRulesetAdmitsInbound(t *testing.T) {
	if err := policy.ValidateRuleset(goodRuleset(), refs); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRulesetRejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*policy.Ruleset)
		want   error
		path   string
	}{
		{"outbound", func(r *policy.Ruleset) { r.Rules[0].Direction = innerwallv1.Direction_DIRECTION_OUTBOUND }, policy.ErrDirectionOutbound, "rules[0].direction"},
		{"unspecified direction", func(r *policy.Ruleset) { r.Rules[0].Direction = innerwallv1.Direction_DIRECTION_UNSPECIFIED }, policy.ErrDirectionUnspecified, "rules[0].direction"},
		{"empty scope", func(r *policy.Ruleset) { r.Scope = nil }, policy.ErrEmptySelector, "scope"},
		{"scope match without values", func(r *policy.Ruleset) { r.Scope = policy.Selector{"role": {}} }, policy.ErrEmptyLabelValues, "scope[role]"},
		{"empty peer selector", func(r *policy.Ruleset) { r.Rules[0].Peers[0].Workloads = policy.Selector{} }, policy.ErrEmptySelector, "rules[0].peers[0].workloads"},
		{"no peers", func(r *policy.Ruleset) { r.Rules[0].Peers = nil }, policy.ErrNoPeers, "rules[0].peers"},
		{"peer with two kinds", func(r *policy.Ruleset) { r.Rules[0].Peers[0].CIDR = "10.0.0.0/8" }, policy.ErrBadPeer, "rules[0].peers[0]"},
		{"bad cidr", func(r *policy.Ruleset) {
			r.Rules[0].Peers = []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "10.0.0.0/33"}}
		}, policy.ErrBadCIDR, "rules[0].peers[0].cidr"},
		{"bare address is not a cidr", func(r *policy.Ruleset) { r.Rules[0].Peers = []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "10.0.0.1"}} }, policy.ErrBadCIDR, "rules[0].peers[0].cidr"},
		{"unknown address group", func(r *policy.Ruleset) {
			r.Rules[0].Peers = []policy.Peer{{Kind: policy.PeerAddressGroup, AddressGroupID: uuid.New()}}
		}, policy.ErrUnknownAddressGroup, "rules[0].peers[0].address_group"},
		{"unknown service", func(r *policy.Ruleset) { r.Rules[0].ServiceIDs = []uuid.UUID{uuid.New()} }, policy.ErrUnknownService, "rules[0].services[0]"},
		{"no services", func(r *policy.Ruleset) { r.Rules[0].ServiceIDs = nil }, policy.ErrNoServices, "rules[0].services"},
		{"inverted port range", func(r *policy.Ruleset) {
			r.Rules[0].Entries = []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 90, End: 80}}}}
		}, policy.ErrBadPortRange, "rules[0].entries[0].ports[0]"},
		{"port above 65535", func(r *policy.Ruleset) {
			r.Rules[0].Entries = []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_UDP, Ports: []policy.PortRange{{Start: 1, End: 70000}}}}
		}, policy.ErrBadPortRange, "rules[0].entries[0].ports[0]"},
		{"icmp with ports", func(r *policy.Ruleset) {
			r.Rules[0].Entries = []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_ICMP, Ports: []policy.PortRange{{Start: 1, End: 1}}}}
		}, policy.ErrPortsOnPortless, "rules[0].entries[0].ports"},
		{"unspecified protocol", func(r *policy.Ruleset) {
			r.Rules[0].Entries = []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_UNSPECIFIED}}
		}, policy.ErrBadProtocol, "rules[0].entries[0].protocol"},
		{"empty name", func(r *policy.Ruleset) { r.Name = "" }, policy.ErrEmptyName, "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := goodRuleset()
			tc.mutate(rs)
			err := policy.ValidateRuleset(rs, refs)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var ve *policy.ValidationError
			if !errors.As(err, &ve) || ve.Path != tc.path {
				t.Fatalf("path = %v, want %q", err, tc.path)
			}
			if !strings.HasPrefix(err.Error(), tc.path+": ") {
				t.Fatalf("message %q does not lead with the path", err.Error())
			}
		})
	}
}

// One pass returns every finding it can establish: a reference that
// resolves to nothing is reported beside the shape faults of the same
// rule, never instead of them, and each problem is counted once.
func TestValidateReportsDocumentFaultsWithAdmission(t *testing.T) {
	names := policy.NewNames([]policy.Service{{ID: svcID, Name: "postgres"}}, []policy.AddressGroup{{ID: groupID, Name: "office"}})
	unresolvedAndOutOfRange := policy.RuleDoc{
		Direction: "inbound",
		Peers:     []policy.PeerDoc{{AddressGroup: "10.0.0.999/8!!"}},
		Entries:   []policy.EntryDoc{{Protocol: "tcp", Ports: []string{"99999"}}},
	}
	want := map[string]error{
		"peers[0].address_group": policy.ErrUnknownAddressGroup,
		"entries[0].ports[0]":    policy.ErrBadPortRange,
	}
	check := func(t *testing.T, err error, prefix string) {
		t.Helper()
		f := policy.AsFindings(err)
		if f == nil {
			t.Fatal("admitted")
		}
		if len(f.Errors) != len(want) {
			t.Fatalf("%d findings, want %d: %v", len(f.Errors), len(want), err)
		}
		for _, e := range f.Errors {
			if w, ok := want[strings.TrimPrefix(e.Path, prefix)]; !ok || !errors.Is(e, w) {
				t.Fatalf("unexpected finding %v (all: %v)", e, err)
			}
		}
	}
	t.Run("ruleset", func(t *testing.T) {
		rs := policy.RulesetFromDoc(&policy.RulesetDoc{Name: "mixed", Scope: map[string][]string{"role": {"db"}}, Rules: []policy.RuleDoc{unresolvedAndOutOfRange}}, names)
		check(t, policy.ValidateRuleset(rs, refs), "rules[0].")
	})
	t.Run("rule", func(t *testing.T) {
		check(t, policy.ValidateRule(policy.RuleFromDoc(&unresolvedAndOutOfRange, names), refs), "")
	})
	t.Run("ports keep their places", func(t *testing.T) {
		// An unreadable port spec does not shift the ones after it: the
		// range fault is reported at the index the document gave it.
		doc := policy.RuleDoc{Direction: "inbound", Peers: []policy.PeerDoc{{CIDR: "10.0.0.0/8"}}, Entries: []policy.EntryDoc{{Protocol: "tcp", Ports: []string{"many", "99999"}}}}
		f := policy.AsFindings(policy.ValidateRule(policy.RuleFromDoc(&doc, names), refs))
		if f == nil || len(f.Errors) != 2 || f.Errors[0].Path != "entries[0].ports[0]" || !errors.Is(f.Errors[0], policy.ErrBadPortSpec) || f.Errors[1].Path != "entries[0].ports[1]" || !errors.Is(f.Errors[1], policy.ErrBadPortRange) {
			t.Fatalf("findings = %v", f)
		}
	})
}

func TestValidateServiceAndAddressGroup(t *testing.T) {
	ok := &policy.Service{Name: "postgres", Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 5432, End: 5432}}}}}
	if err := policy.ValidateService(ok); err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateService(&policy.Service{Name: "x"}); !errors.Is(err, policy.ErrNoEntries) {
		t.Fatalf("err = %v", err)
	}
	if err := policy.ValidateService(&policy.Service{Name: "icmp", Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_ICMP}}}); err != nil {
		t.Fatalf("icmp without ports rejected: %v", err)
	}
	if err := policy.ValidateAddressGroup(&policy.AddressGroup{Name: "corp", CIDRs: []string{"10.0.0.0/8", "fd00::/8"}}); err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateAddressGroup(&policy.AddressGroup{Name: "corp", CIDRs: []string{"10.0.0.0/8", "nope"}}); !errors.Is(err, policy.ErrBadCIDR) {
		t.Fatalf("err = %v", err)
	}
	if err := policy.ValidateAddressGroup(&policy.AddressGroup{Name: "corp"}); !errors.Is(err, policy.ErrNoCIDRs) {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectorSemantics(t *testing.T) {
	labels := map[string]string{"role": "db", "env": "prod"}
	cases := []struct {
		sel  policy.Selector
		want bool
	}{
		{policy.Selector{}, false}, // empty matches nothing
		{nil, false},
		{policy.Selector{"role": {"db"}}, true},
		{policy.Selector{"role": {"web", "db"}}, true},               // OR within values
		{policy.Selector{"role": {"db"}, "env": {"prod"}}, true},     // AND across keys
		{policy.Selector{"role": {"db"}, "env": {"staging"}}, false}, // AND fails on one key
		{policy.Selector{"role": {"db"}, "tier": {"gold"}}, false},   // missing key
		{policy.Selector{"role": {}}, false},                         // no values can never match
	}
	for _, tc := range cases {
		if got := tc.sel.Matches(labels); got != tc.want {
			t.Errorf("%v.Matches = %v, want %v", tc.sel, got, tc.want)
		}
	}
}
