package compiler

import (
	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// Inputs is everything a render reads, loaded in one transaction so that
// every workload's policy is rendered against the same state.
type Inputs struct {
	Workloads     []registry.Workload
	Rulesets      []policy.Ruleset
	Services      []policy.Service
	AddressGroups []policy.AddressGroup
}

// Render produces the policy of every workload in the inputs. Versions are
// left at zero: assigning them is Plan's job, because a version is a
// statement about change and rendering knows nothing about the past.
func Render(in *Inputs) map[identity.WorkloadID]*innerwallv1.WorkloadPolicy {
	r := newResolver(in)
	out := make(map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, len(in.Workloads))
	for i := range in.Workloads {
		w := &in.Workloads[i]
		out[w.ID] = r.renderWorkload(w)
	}
	return out
}

type resolver struct {
	in       *Inputs
	labels   []map[string]string // parallel to in.Workloads
	services map[uuid.UUID]*policy.Service
	groups   map[uuid.UUID]*policy.AddressGroup
}

func newResolver(in *Inputs) *resolver {
	r := &resolver{
		in:       in,
		labels:   make([]map[string]string, len(in.Workloads)),
		services: make(map[uuid.UUID]*policy.Service, len(in.Services)),
		groups:   make(map[uuid.UUID]*policy.AddressGroup, len(in.AddressGroups)),
	}
	for i := range in.Workloads {
		r.labels[i] = in.Workloads[i].LabelMap()
	}
	for i := range in.Services {
		r.services[in.Services[i].ID] = &in.Services[i]
	}
	for i := range in.AddressGroups {
		r.groups[in.AddressGroups[i].ID] = &in.AddressGroups[i]
	}
	return r
}

func (r *resolver) renderWorkload(w *registry.Workload) *innerwallv1.WorkloadPolicy {
	mode := w.Mode
	if mode == innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED {
		mode = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY
	}
	p := &innerwallv1.WorkloadPolicy{Mode: mode}
	labels := w.LabelMap()
	for i := range r.in.Rulesets {
		rs := &r.in.Rulesets[i]
		if !rs.Enabled || !rs.Scope.Matches(labels) {
			continue
		}
		for j := range rs.Rules {
			rule := &rs.Rules[j]
			// Inbound only (ADR-0010). Admission already refuses anything
			// else; the renderer refuses again so the rule holds even if a
			// row reached the table by another path.
			if !rule.Enabled || rule.Direction != innerwallv1.Direction_DIRECTION_INBOUND {
				continue
			}
			p.InboundRules = append(p.InboundRules, r.renderRule(rule)...)
		}
	}
	return rendered.Canonical(p)
}

// renderRule expands one authored rule into one resolved rule per protocol.
// Peers are shared across the protocols; the rule id carries the protocol
// so that each resolved rule is individually addressable by a delta.
func (r *resolver) renderRule(rule *policy.Rule) []*innerwallv1.ResolvedRule {
	peers := r.resolvePeers(rule.Peers)
	entries := r.expandServices(rule)
	out := make([]*innerwallv1.ResolvedRule, 0, len(entries))
	for _, proto := range []innerwallv1.Protocol{innerwallv1.Protocol_PROTOCOL_TCP, innerwallv1.Protocol_PROTOCOL_UDP, innerwallv1.Protocol_PROTOCOL_ICMP} {
		ports, ok := entries[proto]
		if !ok {
			continue
		}
		rr := &innerwallv1.ResolvedRule{
			RuleId:    rendered.RuleID(rule.ID.String(), proto),
			Protocol:  proto,
			PeerCidrs: append([]string(nil), peers...),
		}
		for _, pr := range ports {
			rr.Ports = append(rr.Ports, &innerwallv1.PortRange{Start: pr.Start, End: pr.End})
		}
		out = append(out, rr)
	}
	return out
}

// resolvePeers turns the peer selectors of a rule into concrete CIDRs:
// managed workloads as host routes of their current addresses, address
// groups as their CIDRs, literals as themselves. Deduplication and ordering
// are left to the canonical form.
func (r *resolver) resolvePeers(peers []policy.Peer) []string {
	var out []string
	for i := range peers {
		peer := &peers[i]
		switch peer.Kind {
		case policy.PeerWorkloads:
			for j := range r.in.Workloads {
				if !peer.Workloads.Matches(r.labels[j]) {
					continue
				}
				for _, addr := range r.in.Workloads[j].Addresses {
					out = append(out, rendered.HostRoute(addr))
				}
			}
		case policy.PeerAddressGroup:
			if g, ok := r.groups[peer.AddressGroupID]; ok {
				out = append(out, g.CIDRs...)
			}
		case policy.PeerCIDR:
			out = append(out, rendered.NormalizeCIDR(peer.CIDR))
		case policy.PeerUnspecified:
		}
	}
	return out
}

// expandServices merges a rule's referenced and inline service entries
// into port lists per protocol. A nil list means every port of the
// protocol, which any entry with no ports establishes for its protocol.
func (r *resolver) expandServices(rule *policy.Rule) map[innerwallv1.Protocol][]policy.PortRange {
	out := map[innerwallv1.Protocol][]policy.PortRange{}
	allPorts := map[innerwallv1.Protocol]bool{}
	add := func(e *policy.ServiceEntry) {
		if len(e.Ports) == 0 {
			allPorts[e.Protocol] = true
			out[e.Protocol] = nil
			return
		}
		if allPorts[e.Protocol] {
			return
		}
		out[e.Protocol] = append(out[e.Protocol], e.Ports...)
	}
	for _, id := range rule.ServiceIDs {
		if s, ok := r.services[id]; ok {
			for i := range s.Entries {
				add(&s.Entries[i])
			}
		}
	}
	for i := range rule.Entries {
		add(&rule.Entries[i])
	}
	return out
}
