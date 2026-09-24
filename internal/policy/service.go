package policy

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Renderer is what runs after every admitted mutation: it re-renders every
// workload's policy, persists the ones whose rendered output changed, and
// lets the streams push them (ADR-0018). The authoring service holds it so
// a mutation can never be persisted without triggering it.
type Renderer interface {
	Render(ctx context.Context) error
}

// Authoring is the mutation surface for the authored model: admission, then
// persistence, then rendering. The command line and the operator surface
// go through it and nothing else writes the authored tables; admission
// runs here, so the two transports cannot diverge on what is admitted.
//
// Updates and deletes take the version the caller last read (expect) and
// refuse with a *VersionMismatchError when the object has moved; an empty
// expect writes unconditionally, which is the command line's default.
type Authoring struct {
	Store    Store
	Renderer Renderer
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

func (a *Authoring) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Authoring) render(ctx context.Context) error {
	if a.Renderer == nil {
		return nil
	}
	if err := a.Renderer.Render(ctx); err != nil {
		return fmt.Errorf("policy: change persisted but rendering failed: %w", err)
	}
	return nil
}

// References loads the ids admission resolves against.
func (a *Authoring) References(ctx context.Context) (References, error) {
	refs := References{Services: map[uuid.UUID]struct{}{}, AddressGroups: map[uuid.UUID]struct{}{}}
	services, err := a.Store.ListServices(ctx)
	if err != nil {
		return refs, err
	}
	for _, s := range services {
		refs.Services[s.ID] = struct{}{}
	}
	groups, err := a.Store.ListAddressGroups(ctx)
	if err != nil {
		return refs, err
	}
	for _, g := range groups {
		refs.AddressGroups[g.ID] = struct{}{}
	}
	return refs, nil
}

// CreateService admits and persists a new service, assigning its id.
func (a *Authoring) CreateService(ctx context.Context, s *Service) error {
	if err := ValidateService(s); err != nil {
		return err
	}
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	s.CreatedAt, s.UpdatedAt, s.Version = a.now(), a.now(), 1
	if err := a.Store.CreateService(ctx, s); err != nil {
		return err
	}
	return a.render(ctx)
}

// UpdateService admits and persists a changed service.
func (a *Authoring) UpdateService(ctx context.Context, s *Service, expect string) error {
	if err := ValidateService(s); err != nil {
		return err
	}
	s.UpdatedAt = a.now()
	if err := a.Store.UpdateService(ctx, s, expect); err != nil {
		return err
	}
	return a.render(ctx)
}

// DeleteService removes a service no rule references.
func (a *Authoring) DeleteService(ctx context.Context, id uuid.UUID, expect string) error {
	if err := a.Store.DeleteService(ctx, id, expect); err != nil {
		return err
	}
	return a.render(ctx)
}

// CreateAddressGroup admits and persists a new address group, assigning
// its id and canonicalizing its CIDRs.
func (a *Authoring) CreateAddressGroup(ctx context.Context, g *AddressGroup) error {
	if err := ValidateAddressGroup(g); err != nil {
		return err
	}
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	g.CIDRs = canonicalCIDRs(g.CIDRs)
	g.CreatedAt, g.UpdatedAt, g.Version = a.now(), a.now(), 1
	if err := a.Store.CreateAddressGroup(ctx, g); err != nil {
		return err
	}
	return a.render(ctx)
}

// UpdateAddressGroup admits and persists a changed address group.
func (a *Authoring) UpdateAddressGroup(ctx context.Context, g *AddressGroup, expect string) error {
	if err := ValidateAddressGroup(g); err != nil {
		return err
	}
	g.CIDRs = canonicalCIDRs(g.CIDRs)
	g.UpdatedAt = a.now()
	if err := a.Store.UpdateAddressGroup(ctx, g, expect); err != nil {
		return err
	}
	return a.render(ctx)
}

// DeleteAddressGroup removes an address group no rule references.
func (a *Authoring) DeleteAddressGroup(ctx context.Context, id uuid.UUID, expect string) error {
	if err := a.Store.DeleteAddressGroup(ctx, id, expect); err != nil {
		return err
	}
	return a.render(ctx)
}

// CreateRuleset admits and persists a new ruleset, assigning ids to it and
// to any rule that has none. Every rule is created now.
func (a *Authoring) CreateRuleset(ctx context.Context, rs *Ruleset) error {
	refs, err := a.References(ctx)
	if err != nil {
		return err
	}
	if err := ValidateRuleset(rs, refs); err != nil {
		return err
	}
	if rs.ID == uuid.Nil {
		rs.ID = uuid.New()
	}
	assignRuleIDs(rs)
	now := a.now()
	rs.CreatedAt, rs.UpdatedAt, rs.Version = now, now, 1
	for i := range rs.Rules {
		rs.Rules[i].CreatedAt, rs.Rules[i].UpdatedAt, rs.Rules[i].Version = now, now, 1
	}
	if err := a.Store.CreateRuleset(ctx, rs); err != nil {
		return err
	}
	return a.render(ctx)
}

// UpdateRuleset admits and persists a changed ruleset. Rules that keep
// their id keep their provenance across the edit: their CreatedAt is
// carried from the persisted rule, and their UpdatedAt and Version advance
// only when the rule itself changed. Rules without an id are new.
func (a *Authoring) UpdateRuleset(ctx context.Context, rs *Ruleset, expect string) error {
	refs, err := a.References(ctx)
	if err != nil {
		return err
	}
	if err := ValidateRuleset(rs, refs); err != nil {
		return err
	}
	existing, err := a.Store.GetRuleset(ctx, rs.ID)
	if err != nil {
		return err
	}
	assignRuleIDs(rs)
	now := a.now()
	carryRuleProvenance(existing, rs, now)
	rs.CreatedAt, rs.UpdatedAt = existing.CreatedAt, now
	if err := a.Store.UpdateRuleset(ctx, rs, expect); err != nil {
		return err
	}
	return a.render(ctx)
}

// DeleteRuleset removes a ruleset and its rules.
func (a *Authoring) DeleteRuleset(ctx context.Context, id uuid.UUID, expect string) error {
	if err := a.Store.DeleteRuleset(ctx, id, expect); err != nil {
		return err
	}
	return a.render(ctx)
}

// CreateRule adds one rule to a ruleset. The ruleset is written as a
// unit, conditioned on the version it was read at, so a concurrent edit
// of the same ruleset is refused rather than overwritten. Findings are
// reported relative to the rule.
func (a *Authoring) CreateRule(ctx context.Context, rulesetID uuid.UUID, r *Rule) error {
	rs, err := a.Store.GetRuleset(ctx, rulesetID)
	if err != nil {
		return err
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	rs.Rules = append(rs.Rules, *r)
	if err := a.UpdateRuleset(ctx, rs, FormatVersion(rs.Version)); err != nil {
		return a.ruleWriteError(ctx, rulesetID, r.ID, err, len(rs.Rules)-1)
	}
	*r = rs.Rules[len(rs.Rules)-1]
	return nil
}

// UpdateRule replaces one rule of a ruleset. expect is the rule's own
// version; a stale one is refused with the rule's current version.
func (a *Authoring) UpdateRule(ctx context.Context, rulesetID uuid.UUID, r *Rule, expect string) error {
	rs, err := a.Store.GetRuleset(ctx, rulesetID)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(rs.Rules, func(x Rule) bool { return x.ID == r.ID })
	if idx < 0 {
		return ErrRuleUnknown
	}
	if current := FormatVersion(rs.Rules[idx].Version); expect != "" && expect != current {
		return &VersionMismatchError{Current: current}
	}
	rs.Rules[idx] = *r
	if err := a.UpdateRuleset(ctx, rs, FormatVersion(rs.Version)); err != nil {
		return a.ruleWriteError(ctx, rulesetID, r.ID, err, idx)
	}
	*r = rs.Rules[idx]
	return nil
}

// DeleteRule removes one rule from a ruleset. expect is the rule's own
// version.
func (a *Authoring) DeleteRule(ctx context.Context, rulesetID, ruleID uuid.UUID, expect string) error {
	rs, err := a.Store.GetRuleset(ctx, rulesetID)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(rs.Rules, func(x Rule) bool { return x.ID == ruleID })
	if idx < 0 {
		return ErrRuleUnknown
	}
	if current := FormatVersion(rs.Rules[idx].Version); expect != "" && expect != current {
		return &VersionMismatchError{Current: current}
	}
	rs.Rules = slices.Delete(rs.Rules, idx, idx+1)
	if err := a.UpdateRuleset(ctx, rs, FormatVersion(rs.Version)); err != nil {
		return a.ruleWriteError(ctx, rulesetID, ruleID, err, -1)
	}
	return nil
}

// ruleWriteError restates a ruleset write's failure for a rule-level
// caller: findings are rebased to the rule, and a version mismatch on the
// ruleset (someone else wrote it between the read and the write) is
// reported with the rule's current version, or as unknown when the rule
// is gone.
func (a *Authoring) ruleWriteError(ctx context.Context, rulesetID, ruleID uuid.UUID, err error, idx int) error {
	if f := AsFindings(err); f != nil && idx >= 0 {
		f.Rebase(fmt.Sprintf("rules[%d]", idx))
		return f
	}
	if !errors.Is(err, ErrVersionMismatch) {
		return err
	}
	rs, lerr := a.Store.GetRuleset(ctx, rulesetID)
	if lerr != nil {
		return lerr
	}
	for i := range rs.Rules {
		if rs.Rules[i].ID == ruleID {
			return &VersionMismatchError{Current: FormatVersion(rs.Rules[i].Version)}
		}
	}
	return ErrRuleUnknown
}

// carryRuleProvenance gives every rule of next its instants and version: a
// rule whose id existed in prev keeps its CreatedAt, and keeps its
// UpdatedAt and Version unless its content changed, when the one moves to
// now and the other advances by one; every other rule is created now at
// version 1.
func carryRuleProvenance(prev, next *Ruleset, now time.Time) {
	before := make(map[uuid.UUID]*Rule, len(prev.Rules))
	for i := range prev.Rules {
		before[prev.Rules[i].ID] = &prev.Rules[i]
	}
	for i := range next.Rules {
		r := &next.Rules[i]
		old, ok := before[r.ID]
		if !ok {
			r.CreatedAt, r.UpdatedAt, r.Version = now, now, 1
			continue
		}
		r.CreatedAt = old.CreatedAt
		if RuleEqual(old, r) {
			r.UpdatedAt, r.Version = old.UpdatedAt, old.Version
		} else {
			r.UpdatedAt, r.Version = now, old.Version+1
		}
	}
}

// RuleEqual reports whether two rules say the same thing: same direction,
// enablement, description, peers, services, and entries, in order.
// Timestamps and versions are not compared; they are a consequence of
// content changing.
func RuleEqual(a, b *Rule) bool {
	if a.Direction != b.Direction || a.Enabled != b.Enabled || a.Description != b.Description {
		return false
	}
	if !slices.EqualFunc(a.Peers, b.Peers, peerEqual) || !slices.Equal(a.ServiceIDs, b.ServiceIDs) {
		return false
	}
	return slices.EqualFunc(a.Entries, b.Entries, entryEqual)
}

func peerEqual(a, b Peer) bool {
	if a.Kind != b.Kind || a.AddressGroupID != b.AddressGroupID || a.CIDR != b.CIDR || len(a.Workloads) != len(b.Workloads) {
		return false
	}
	for k, av := range a.Workloads {
		if !slices.Equal(av, b.Workloads[k]) {
			return false
		}
	}
	return true
}

func entryEqual(a, b ServiceEntry) bool {
	return a.Protocol == b.Protocol && slices.Equal(a.Ports, b.Ports)
}

func assignRuleIDs(rs *Ruleset) {
	for i := range rs.Rules {
		if rs.Rules[i].ID == uuid.Nil {
			rs.Rules[i].ID = uuid.New()
		}
		for j := range rs.Rules[i].Peers {
			if rs.Rules[i].Peers[j].Kind == PeerCIDR {
				rs.Rules[i].Peers[j].CIDR = canonicalCIDR(rs.Rules[i].Peers[j].CIDR)
			}
		}
	}
}

func canonicalCIDR(s string) string {
	if pfx, err := netip.ParsePrefix(s); err == nil {
		return pfx.Masked().String()
	}
	return s
}

func canonicalCIDRs(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		c := canonicalCIDR(s)
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}
