package readmodel

import (
	"context"
	"time"

	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// RulesetRef names the authored ruleset a rendered rule came from. It is
// nil on a rendered rule whose authored rule no longer exists, which can
// happen between a delete and the render it triggers. It carries no
// timestamps: nothing persisted records when a rule was created or last
// changed, and the ruleset's instants are not the rule's; per-rule
// instants arrive with the authoring schema.
type RulesetRef struct {
	ID   uuid.UUID
	Name string
}

// RenderedRule is one resolved inbound permission as the agent holds it
// (ADR-0018): its match criteria (peers, protocol, ports), the verdict a
// match receives, and the authored rule it was rendered from. Every
// rendered rule admits; the mode decides what the rest of the traffic
// receives.
type RenderedRule struct {
	ID             string
	AuthoredRuleID string
	Ruleset        *RulesetRef
	Description    string
	Protocol       innerwallv1.Protocol
	// Ports is empty when the rule permits every port of the protocol.
	Ports     []policy.PortRange
	PeerCIDRs []string
	Verdict   innerwallv1.PolicyDecision
}

// RenderedPolicy is a workload's persisted rendered policy: the version
// the agent is served, the mode, when it was rendered (nil when no render
// has happened yet), the verdict traffic no rule admits receives under
// that mode, and the rules. A pure configuration read: no counts and no
// flow joins; the console pairs it with a rollup.
type RenderedPolicy struct {
	Workload        WorkloadRef
	Version         uint64
	Mode            innerwallv1.EnforcementMode
	RenderedAt      *time.Time
	TerminalVerdict innerwallv1.PolicyDecision
	Rules           []RenderedRule
}

// TerminalVerdict is what traffic no rendered rule admits receives under
// a mode: observed in visibility, would_block in simulation, blocked when
// enforced (ADR-0020).
func TerminalVerdict(mode innerwallv1.EnforcementMode) innerwallv1.PolicyDecision {
	switch mode {
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION:
		return innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED:
		return innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED:
		return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED
	default:
		return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED
	}
}

// RenderedPolicy returns a workload's persisted rendered policy, or
// registry.ErrWorkloadUnknown. A workload with no persisted policy yet is
// reported at version zero with no rules and no render instant.
func (s *Reader) RenderedPolicy(ctx context.Context, id identity.WorkloadID) (*RenderedPolicy, error) {
	rec, err := s.Store.GetWorkloadRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &RenderedPolicy{Workload: workloadRef(&rec.Workload), Mode: rec.Mode, RenderedAt: rec.LatestRenderedAt, Rules: []RenderedRule{}}
	p, err := s.Store.GetWorkloadPolicy(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		out.TerminalVerdict = TerminalVerdict(out.Mode)
		return out, nil
	}
	p = rendered.Canonical(p)
	out.Version, out.Mode = p.GetVersion(), p.GetMode()
	out.TerminalVerdict = TerminalVerdict(out.Mode)
	rulesets, err := s.Store.ListRulesets(ctx)
	if err != nil {
		return nil, err
	}
	authored := map[string]*policy.Rule{}
	owner := map[string]*policy.Ruleset{}
	for i := range rulesets {
		rs := &rulesets[i]
		for j := range rs.Rules {
			authored[rs.Rules[j].ID.String()] = &rs.Rules[j]
			owner[rs.Rules[j].ID.String()] = rs
		}
	}
	for _, rr := range p.GetInboundRules() {
		rule := RenderedRule{
			ID: rr.GetRuleId(), Protocol: rr.GetProtocol(), PeerCIDRs: append([]string{}, rr.GetPeerCidrs()...),
			Ports: make([]policy.PortRange, 0, len(rr.GetPorts())), Verdict: innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED,
		}
		for _, pr := range rr.GetPorts() {
			rule.Ports = append(rule.Ports, policy.PortRange{Start: pr.GetStart(), End: pr.GetEnd()})
		}
		if authoredID, _, err := rendered.Provenance(rr.GetRuleId()); err == nil {
			rule.AuthoredRuleID = authoredID
			if a, ok := authored[authoredID]; ok {
				rule.Description = a.Description
				rs := owner[authoredID]
				rule.Ruleset = &RulesetRef{ID: rs.ID, Name: rs.Name}
			}
		}
		out.Rules = append(out.Rules, rule)
	}
	return out, nil
}

// provenance splits a resolved-rule id into its authored id and protocol.
func provenance(ruleID string) (string, innerwallv1.Protocol, error) {
	return rendered.Provenance(ruleID)
}
