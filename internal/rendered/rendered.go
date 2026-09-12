// Package rendered is the algebra of the rendered policy model: the
// canonical form of a WorkloadPolicy, the diff between two consecutive
// versions expressed as the wire contract's DeltaChange set, and the
// application of such a delta to a policy (ADR-0015, ADR-0018).
//
// The control plane uses Diff to decide whether a workload's version
// advances and what to push; the agent uses Apply to turn a delta into the
// next complete policy before installing it atomically. Both sides share
// this one implementation so that the contract's defining property,
//
//	Apply(snapshot(v_n), Diff(v_n, v_{n+1})) == Canonical(snapshot(v_{n+1})),
//
// is tested in one place and holds on both ends of the wire. The package
// depends only on the generated wire contract; it links into the agent.
package rendered

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// ErrBaseMismatch is returned by Apply when the delta's from_version is not
// the base policy's version. Per the wire contract the delta must then be
// discarded and a snapshot requested; it is never applied to a different
// base.
var ErrBaseMismatch = errors.New("rendered: delta base version does not match the applied version")

// ErrUnknownRule is returned by Apply when a peer change names a rule the
// base policy does not contain.
var ErrUnknownRule = errors.New("rendered: peer change names a rule the policy does not contain")

// ErrMalformedChange is returned by Apply for a change with no variant set.
var ErrMalformedChange = errors.New("rendered: delta change carries no variant")

// Empty returns the policy an agent holds before any snapshot: version 0,
// visibility mode, no rules. It is the base every first snapshot replaces.
func Empty() *innerwallv1.WorkloadPolicy {
	return &innerwallv1.WorkloadPolicy{Version: 0, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY}
}

// Canonical returns a copy of p in canonical form: rules sorted by id, each
// rule's peers deduplicated and sorted, each rule's ports deduplicated and
// sorted. Two policies that permit the same traffic have identical canonical
// forms, so Equal on canonical forms is exact, and a persisted policy and a
// freshly rendered one can be compared byte for byte.
func Canonical(p *innerwallv1.WorkloadPolicy) *innerwallv1.WorkloadPolicy {
	if p == nil {
		return Empty()
	}
	out := &innerwallv1.WorkloadPolicy{Version: p.GetVersion(), Mode: p.GetMode()}
	byID := make(map[string]*innerwallv1.ResolvedRule, len(p.GetInboundRules()))
	for _, r := range p.GetInboundRules() {
		// A later rule with the same id replaces an earlier one, matching
		// the semantics of upsert_rule on the wire.
		byID[r.GetRuleId()] = canonicalRule(r)
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.InboundRules = append(out.InboundRules, byID[id])
	}
	return out
}

func canonicalRule(r *innerwallv1.ResolvedRule) *innerwallv1.ResolvedRule {
	out := &innerwallv1.ResolvedRule{RuleId: r.GetRuleId(), Protocol: r.GetProtocol()}
	out.PeerCidrs = canonicalCIDRs(r.GetPeerCidrs())
	out.Ports = canonicalPorts(r.GetPorts())
	return out
}

// canonicalCIDRs normalizes each prefix to its masked textual form,
// deduplicates, and sorts. A prefix that does not parse is kept verbatim so
// that a malformed value is visible rather than silently dropped; admission
// validation upstream prevents malformed values from being rendered.
func canonicalCIDRs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, NormalizeCIDR(s))
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// NormalizeCIDR returns the canonical textual form of a prefix: the masked
// network address with its prefix length. A bare address becomes a host
// route (/32 or /128). Input that does not parse is returned unchanged.
func NormalizeCIDR(s string) string {
	s = strings.TrimSpace(s)
	if pfx, err := netip.ParsePrefix(s); err == nil {
		return pfx.Masked().String()
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		return HostRoute(addr)
	}
	return s
}

// HostRoute returns the /32 or /128 prefix covering exactly addr.
func HostRoute(addr netip.Addr) string {
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()).String()
}

func canonicalPorts(in []*innerwallv1.PortRange) []*innerwallv1.PortRange {
	if len(in) == 0 {
		return nil
	}
	out := make([]*innerwallv1.PortRange, 0, len(in))
	for _, p := range in {
		out = append(out, &innerwallv1.PortRange{Start: p.GetStart(), End: p.GetEnd()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GetStart() != out[j].GetStart() {
			return out[i].GetStart() < out[j].GetStart()
		}
		return out[i].GetEnd() < out[j].GetEnd()
	})
	return slices.CompactFunc(out, func(a, b *innerwallv1.PortRange) bool {
		return a.GetStart() == b.GetStart() && a.GetEnd() == b.GetEnd()
	})
}

// Equal reports whether a and b permit the same traffic in the same mode.
// Versions are not compared: equality is about content, and the version is
// a consequence of content changing.
func Equal(a, b *innerwallv1.WorkloadPolicy) bool {
	ca, cb := Canonical(a), Canonical(b)
	ca.Version, cb.Version = 0, 0
	return proto.Equal(ca, cb)
}

// Marshal serializes the canonical form of p deterministically, so that the
// persisted bytes of equal policies are equal.
func Marshal(p *innerwallv1.WorkloadPolicy) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(Canonical(p))
}

// Unmarshal parses a persisted policy.
func Unmarshal(b []byte) (*innerwallv1.WorkloadPolicy, error) {
	p := &innerwallv1.WorkloadPolicy{}
	if err := proto.Unmarshal(b, p); err != nil {
		return nil, fmt.Errorf("rendered: parsing policy: %w", err)
	}
	return p, nil
}

// Diff returns the minimal DeltaChange set that turns from into to. It is
// empty exactly when Equal(from, to). The result is deterministic: a mode
// change first, then rule changes ordered by rule id, so that two renders
// of the same pair produce the same delta.
//
// A rule present only in to is an upsert; a rule present only in from is a
// removal; a rule in both whose protocol or ports differ is an upsert
// (wholesale replacement); a rule in both that differs only in peers is an
// add_peers and/or remove_peers, which is the common case when a workload
// joins or leaves a label.
func Diff(from, to *innerwallv1.WorkloadPolicy) []*innerwallv1.DeltaChange {
	from, to = Canonical(from), Canonical(to)
	var changes []*innerwallv1.DeltaChange
	if from.GetMode() != to.GetMode() {
		changes = append(changes, &innerwallv1.DeltaChange{Change: &innerwallv1.DeltaChange_SetMode{SetMode: to.GetMode()}})
	}
	fromRules := indexRules(from)
	toRules := indexRules(to)
	ids := make([]string, 0, len(fromRules)+len(toRules))
	for id := range fromRules {
		ids = append(ids, id)
	}
	for id := range toRules {
		if _, ok := fromRules[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		old, inOld := fromRules[id]
		cur, inNew := toRules[id]
		switch {
		case inNew && !inOld:
			changes = append(changes, &innerwallv1.DeltaChange{Change: &innerwallv1.DeltaChange_UpsertRule{UpsertRule: cur}})
		case inOld && !inNew:
			changes = append(changes, &innerwallv1.DeltaChange{Change: &innerwallv1.DeltaChange_RemoveRuleId{RemoveRuleId: id}})
		case old.GetProtocol() != cur.GetProtocol() || !portsEqual(old.GetPorts(), cur.GetPorts()):
			changes = append(changes, &innerwallv1.DeltaChange{Change: &innerwallv1.DeltaChange_UpsertRule{UpsertRule: cur}})
		default:
			added, removed := setDiff(old.GetPeerCidrs(), cur.GetPeerCidrs())
			if len(added) > 0 {
				changes = append(changes, &innerwallv1.DeltaChange{Change: &innerwallv1.DeltaChange_AddPeers{AddPeers: &innerwallv1.PeerChange{RuleId: id, PeerCidrs: added}}})
			}
			if len(removed) > 0 {
				changes = append(changes, &innerwallv1.DeltaChange{Change: &innerwallv1.DeltaChange_RemovePeers{RemovePeers: &innerwallv1.PeerChange{RuleId: id, PeerCidrs: removed}}})
			}
		}
	}
	return changes
}

// Delta builds the PolicyDelta that moves an agent from from to to. The
// version numbers are taken from the two policies.
func Delta(from, to *innerwallv1.WorkloadPolicy) *innerwallv1.PolicyDelta {
	return &innerwallv1.PolicyDelta{
		FromVersion: from.GetVersion(),
		ToVersion:   to.GetVersion(),
		Changes:     Diff(from, to),
	}
}

// Apply returns the canonical policy that results from applying delta to
// base. It returns ErrBaseMismatch when the delta was computed against a
// different version than base carries, and never modifies base: a failed
// apply leaves the caller holding exactly what it had, which is what lets
// the agent acknowledge FAILED and stay on its last good version.
func Apply(base *innerwallv1.WorkloadPolicy, delta *innerwallv1.PolicyDelta) (*innerwallv1.WorkloadPolicy, error) {
	out := Canonical(base)
	if out.GetVersion() != delta.GetFromVersion() {
		return nil, fmt.Errorf("%w: applied %d, delta from %d", ErrBaseMismatch, out.GetVersion(), delta.GetFromVersion())
	}
	rules := indexRules(out)
	for i, ch := range delta.GetChanges() {
		switch c := ch.GetChange().(type) {
		case *innerwallv1.DeltaChange_UpsertRule:
			rules[c.UpsertRule.GetRuleId()] = canonicalRule(c.UpsertRule)
		case *innerwallv1.DeltaChange_RemoveRuleId:
			delete(rules, c.RemoveRuleId)
		case *innerwallv1.DeltaChange_AddPeers:
			r, ok := rules[c.AddPeers.GetRuleId()]
			if !ok {
				return nil, fmt.Errorf("%w: change %d adds peers to %q", ErrUnknownRule, i, c.AddPeers.GetRuleId())
			}
			r.PeerCidrs = canonicalCIDRs(append(append([]string(nil), r.GetPeerCidrs()...), c.AddPeers.GetPeerCidrs()...))
		case *innerwallv1.DeltaChange_RemovePeers:
			r, ok := rules[c.RemovePeers.GetRuleId()]
			if !ok {
				return nil, fmt.Errorf("%w: change %d removes peers from %q", ErrUnknownRule, i, c.RemovePeers.GetRuleId())
			}
			drop := make(map[string]struct{}, len(c.RemovePeers.GetPeerCidrs()))
			for _, p := range c.RemovePeers.GetPeerCidrs() {
				drop[NormalizeCIDR(p)] = struct{}{}
			}
			kept := r.GetPeerCidrs()[:0:0]
			for _, p := range r.GetPeerCidrs() {
				if _, gone := drop[p]; !gone {
					kept = append(kept, p)
				}
			}
			r.PeerCidrs = kept
		case *innerwallv1.DeltaChange_SetMode:
			out.Mode = c.SetMode
		default:
			return nil, fmt.Errorf("%w: change %d", ErrMalformedChange, i)
		}
	}
	out.InboundRules = out.InboundRules[:0]
	for _, r := range rules {
		out.InboundRules = append(out.InboundRules, r)
	}
	out.Version = delta.GetToVersion()
	return Canonical(out), nil
}

// Describe renders a change as a short human-readable string for logs.
func Describe(ch *innerwallv1.DeltaChange) string {
	switch c := ch.GetChange().(type) {
	case *innerwallv1.DeltaChange_UpsertRule:
		return fmt.Sprintf("upsert rule %s %s ports=%s peers=%d", c.UpsertRule.GetRuleId(), protoName(c.UpsertRule.GetProtocol()), portsString(c.UpsertRule.GetPorts()), len(c.UpsertRule.GetPeerCidrs()))
	case *innerwallv1.DeltaChange_RemoveRuleId:
		return "remove rule " + c.RemoveRuleId
	case *innerwallv1.DeltaChange_AddPeers:
		return fmt.Sprintf("add peers %s +%s", c.AddPeers.GetRuleId(), strings.Join(c.AddPeers.GetPeerCidrs(), ","))
	case *innerwallv1.DeltaChange_RemovePeers:
		return fmt.Sprintf("remove peers %s -%s", c.RemovePeers.GetRuleId(), strings.Join(c.RemovePeers.GetPeerCidrs(), ","))
	case *innerwallv1.DeltaChange_SetMode:
		return "set mode " + c.SetMode.String()
	default:
		return "malformed change"
	}
}

func protoName(p innerwallv1.Protocol) string {
	return strings.ToLower(strings.TrimPrefix(p.String(), "PROTOCOL_"))
}

func portsString(ports []*innerwallv1.PortRange) string {
	if len(ports) == 0 {
		return "any"
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		if p.GetStart() == p.GetEnd() {
			parts = append(parts, fmt.Sprint(p.GetStart()))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", p.GetStart(), p.GetEnd()))
		}
	}
	return strings.Join(parts, ",")
}

func indexRules(p *innerwallv1.WorkloadPolicy) map[string]*innerwallv1.ResolvedRule {
	m := make(map[string]*innerwallv1.ResolvedRule, len(p.GetInboundRules()))
	for _, r := range p.GetInboundRules() {
		m[r.GetRuleId()] = r
	}
	return m
}

func portsEqual(a, b []*innerwallv1.PortRange) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].GetStart() != b[i].GetStart() || a[i].GetEnd() != b[i].GetEnd() {
			return false
		}
	}
	return true
}

// setDiff returns the elements of cur not in old and of old not in cur.
// Both inputs are canonical (sorted, deduplicated).
func setDiff(old, cur []string) (added, removed []string) {
	oldSet := make(map[string]struct{}, len(old))
	for _, s := range old {
		oldSet[s] = struct{}{}
	}
	curSet := make(map[string]struct{}, len(cur))
	for _, s := range cur {
		curSet[s] = struct{}{}
		if _, ok := oldSet[s]; !ok {
			added = append(added, s)
		}
	}
	for _, s := range old {
		if _, ok := curSet[s]; !ok {
			removed = append(removed, s)
		}
	}
	return added, removed
}
