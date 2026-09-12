package policy

import (
	"context"
	"fmt"
	"net/netip"
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
// persistence, then rendering. The command line and, later, the API façade
// go through it and nothing else writes the authored tables.
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

// references loads the ids admission resolves against.
func (a *Authoring) references(ctx context.Context) (References, error) {
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
	s.CreatedAt, s.UpdatedAt = a.now(), a.now()
	if err := a.Store.CreateService(ctx, s); err != nil {
		return err
	}
	return a.render(ctx)
}

// UpdateService admits and persists a changed service.
func (a *Authoring) UpdateService(ctx context.Context, s *Service) error {
	if err := ValidateService(s); err != nil {
		return err
	}
	s.UpdatedAt = a.now()
	if err := a.Store.UpdateService(ctx, s); err != nil {
		return err
	}
	return a.render(ctx)
}

// DeleteService removes a service no rule references.
func (a *Authoring) DeleteService(ctx context.Context, id uuid.UUID) error {
	if err := a.Store.DeleteService(ctx, id); err != nil {
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
	g.CreatedAt, g.UpdatedAt = a.now(), a.now()
	if err := a.Store.CreateAddressGroup(ctx, g); err != nil {
		return err
	}
	return a.render(ctx)
}

// UpdateAddressGroup admits and persists a changed address group.
func (a *Authoring) UpdateAddressGroup(ctx context.Context, g *AddressGroup) error {
	if err := ValidateAddressGroup(g); err != nil {
		return err
	}
	g.CIDRs = canonicalCIDRs(g.CIDRs)
	g.UpdatedAt = a.now()
	if err := a.Store.UpdateAddressGroup(ctx, g); err != nil {
		return err
	}
	return a.render(ctx)
}

// DeleteAddressGroup removes an address group no rule references.
func (a *Authoring) DeleteAddressGroup(ctx context.Context, id uuid.UUID) error {
	if err := a.Store.DeleteAddressGroup(ctx, id); err != nil {
		return err
	}
	return a.render(ctx)
}

// CreateRuleset admits and persists a new ruleset, assigning ids to it and
// to any rule that has none.
func (a *Authoring) CreateRuleset(ctx context.Context, rs *Ruleset) error {
	refs, err := a.references(ctx)
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
	rs.CreatedAt, rs.UpdatedAt = a.now(), a.now()
	if err := a.Store.CreateRuleset(ctx, rs); err != nil {
		return err
	}
	return a.render(ctx)
}

// UpdateRuleset admits and persists a changed ruleset. Rules that keep
// their id keep their provenance across the edit; rules without an id are
// new.
func (a *Authoring) UpdateRuleset(ctx context.Context, rs *Ruleset) error {
	refs, err := a.references(ctx)
	if err != nil {
		return err
	}
	if err := ValidateRuleset(rs, refs); err != nil {
		return err
	}
	assignRuleIDs(rs)
	rs.UpdatedAt = a.now()
	if err := a.Store.UpdateRuleset(ctx, rs); err != nil {
		return err
	}
	return a.render(ctx)
}

// DeleteRuleset removes a ruleset and its rules.
func (a *Authoring) DeleteRuleset(ctx context.Context, id uuid.UUID) error {
	if err := a.Store.DeleteRuleset(ctx, id); err != nil {
		return err
	}
	return a.render(ctx)
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
