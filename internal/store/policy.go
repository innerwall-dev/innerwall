package store

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

var _ policy.Store = (*Store)(nil)

// Postgres error codes the authored model maps to domain errors.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

func isPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

// --- services --------------------------------------------------------------

// CreateService implements policy.Store.
func (s *Store) CreateService(ctx context.Context, svc *policy.Service) error {
	return s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateService(ctx, db.CreateServiceParams{ID: svc.ID, Name: svc.Name, CreatedAt: svc.CreatedAt}); err != nil {
			if isPgCode(err, pgUniqueViolation) {
				return fmt.Errorf("%w: service %q", policy.ErrDuplicateName, svc.Name)
			}
			return fmt.Errorf("store: creating service: %w", err)
		}
		return addServiceEntries(ctx, q, svc)
	})
}

// expected is the version token a conditional statement compares
// against, byte-exact, or nil for an unconditional write.
func expected(expect string) *string {
	if expect == "" {
		return nil
	}
	return &expect
}

// versionMismatch reports why a conditional write touched no row: the
// object is gone (unknown), or it holds a version the caller did not read.
func versionMismatch(err error, unknown error, version int64) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return unknown
	}
	if err != nil {
		return fmt.Errorf("store: looking up object after a refused write: %w", err)
	}
	return &policy.VersionMismatchError{Current: policy.FormatVersion(version)}
}

// UpdateService implements policy.Store.
func (s *Store) UpdateService(ctx context.Context, svc *policy.Service, expect string) error {
	return s.tx(ctx, func(q *db.Queries) error {
		version, err := q.UpdateService(ctx, db.UpdateServiceParams{ID: svc.ID, Name: svc.Name, UpdatedAt: svc.UpdatedAt, Expected: expected(expect)})
		if errors.Is(err, pgx.ErrNoRows) {
			row, err := q.GetService(ctx, svc.ID)
			return versionMismatch(err, policy.ErrServiceUnknown, row.Version)
		}
		if err != nil {
			if isPgCode(err, pgUniqueViolation) {
				return fmt.Errorf("%w: service %q", policy.ErrDuplicateName, svc.Name)
			}
			return fmt.Errorf("store: updating service: %w", err)
		}
		svc.Version = version
		if err := q.DeleteServiceEntries(ctx, svc.ID); err != nil {
			return fmt.Errorf("store: replacing service entries: %w", err)
		}
		return addServiceEntries(ctx, q, svc)
	})
}

func addServiceEntries(ctx context.Context, q *db.Queries, svc *policy.Service) error {
	ordinal := int32(0)
	for _, e := range svc.Entries {
		for _, row := range entryRows(e) {
			if err := q.AddServiceEntry(ctx, db.AddServiceEntryParams{ServiceID: svc.ID, Ordinal: ordinal, Protocol: int32(e.Protocol), PortStart: row[0], PortEnd: row[1]}); err != nil {
				return fmt.Errorf("store: adding service entry: %w", err)
			}
			ordinal++
		}
	}
	return nil
}

// entryRows flattens one entry into (start, end) pairs; an entry without
// ports is one row with NULL bounds.
func entryRows(e policy.ServiceEntry) [][2]*int32 {
	if len(e.Ports) == 0 {
		return [][2]*int32{{nil, nil}}
	}
	out := make([][2]*int32, 0, len(e.Ports))
	for _, p := range e.Ports {
		start, end := int32(p.Start), int32(p.End) //nolint:gosec // validated <= 65535
		out = append(out, [2]*int32{&start, &end})
	}
	return out
}

// entriesFromRows regroups flattened rows by protocol, in first-seen order.
func entriesFromRows(rows []struct {
	Protocol int32
	Start    *int32
	End      *int32
}) []policy.ServiceEntry {
	var out []policy.ServiceEntry
	index := map[int32]int{}
	for _, r := range rows {
		i, ok := index[r.Protocol]
		if !ok {
			i = len(out)
			index[r.Protocol] = i
			out = append(out, policy.ServiceEntry{Protocol: innerwallv1.Protocol(r.Protocol)})
		}
		if r.Start != nil && r.End != nil {
			out[i].Ports = append(out[i].Ports, policy.PortRange{Start: uint32(*r.Start), End: uint32(*r.End)}) //nolint:gosec // non-negative by CHECK
		} else {
			out[i].Ports = nil // all ports wins for the protocol
		}
	}
	return out
}

type flatEntry = struct {
	Protocol int32
	Start    *int32
	End      *int32
}

// DeleteService implements policy.Store.
func (s *Store) DeleteService(ctx context.Context, id uuid.UUID, expect string) error {
	n, err := s.q.DeleteService(ctx, db.DeleteServiceParams{ID: id, Expected: expected(expect)})
	if isPgCode(err, pgForeignKeyViolation) {
		return fmt.Errorf("%w: service %s", policy.ErrInUse, id)
	}
	if err != nil {
		return fmt.Errorf("store: deleting service: %w", err)
	}
	if n == 0 {
		row, err := s.q.GetService(ctx, id)
		return versionMismatch(err, policy.ErrServiceUnknown, row.Version)
	}
	return nil
}

// GetService implements policy.Store.
func (s *Store) GetService(ctx context.Context, id uuid.UUID) (*policy.Service, error) {
	row, err := s.q.GetService(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, policy.ErrServiceUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up service: %w", err)
	}
	entries, err := s.q.ListServiceEntries(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("store: listing service entries: %w", err)
	}
	flat := make([]flatEntry, 0, len(entries))
	for _, e := range entries {
		flat = append(flat, flatEntry{e.Protocol, e.PortStart, e.PortEnd})
	}
	return &policy.Service{ID: row.ID, Name: row.Name, Entries: entriesFromRows(flat), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Version: row.Version}, nil
}

// ListServices implements policy.Store.
func (s *Store) ListServices(ctx context.Context) ([]policy.Service, error) {
	return listServices(ctx, s.q)
}

func listServices(ctx context.Context, q *db.Queries) ([]policy.Service, error) {
	rows, err := q.ListServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing services: %w", err)
	}
	entries, err := q.ListAllServiceEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing service entries: %w", err)
	}
	flat := map[uuid.UUID][]flatEntry{}
	for _, e := range entries {
		flat[e.ServiceID] = append(flat[e.ServiceID], flatEntry{e.Protocol, e.PortStart, e.PortEnd})
	}
	out := make([]policy.Service, 0, len(rows))
	for _, r := range rows {
		out = append(out, policy.Service{ID: r.ID, Name: r.Name, Entries: entriesFromRows(flat[r.ID]), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version})
	}
	return out, nil
}

// --- address groups --------------------------------------------------------

// CreateAddressGroup implements policy.Store.
func (s *Store) CreateAddressGroup(ctx context.Context, g *policy.AddressGroup) error {
	return s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateAddressGroup(ctx, db.CreateAddressGroupParams{ID: g.ID, Name: g.Name, CreatedAt: g.CreatedAt}); err != nil {
			if isPgCode(err, pgUniqueViolation) {
				return fmt.Errorf("%w: address group %q", policy.ErrDuplicateName, g.Name)
			}
			return fmt.Errorf("store: creating address group: %w", err)
		}
		return addGroupCIDRs(ctx, q, g)
	})
}

// UpdateAddressGroup implements policy.Store.
func (s *Store) UpdateAddressGroup(ctx context.Context, g *policy.AddressGroup, expect string) error {
	return s.tx(ctx, func(q *db.Queries) error {
		version, err := q.UpdateAddressGroup(ctx, db.UpdateAddressGroupParams{ID: g.ID, Name: g.Name, UpdatedAt: g.UpdatedAt, Expected: expected(expect)})
		if errors.Is(err, pgx.ErrNoRows) {
			row, err := q.GetAddressGroup(ctx, g.ID)
			return versionMismatch(err, policy.ErrAddressGroupUnknown, row.Version)
		}
		if err != nil {
			if isPgCode(err, pgUniqueViolation) {
				return fmt.Errorf("%w: address group %q", policy.ErrDuplicateName, g.Name)
			}
			return fmt.Errorf("store: updating address group: %w", err)
		}
		g.Version = version
		if err := q.DeleteAddressGroupCIDRs(ctx, g.ID); err != nil {
			return fmt.Errorf("store: replacing address group cidrs: %w", err)
		}
		return addGroupCIDRs(ctx, q, g)
	})
}

func addGroupCIDRs(ctx context.Context, q *db.Queries, g *policy.AddressGroup) error {
	for _, c := range g.CIDRs {
		if err := q.AddAddressGroupCIDR(ctx, db.AddAddressGroupCIDRParams{AddressGroupID: g.ID, Cidr: c}); err != nil {
			return fmt.Errorf("store: adding address group cidr: %w", err)
		}
	}
	return nil
}

// DeleteAddressGroup implements policy.Store.
func (s *Store) DeleteAddressGroup(ctx context.Context, id uuid.UUID, expect string) error {
	n, err := s.q.DeleteAddressGroup(ctx, db.DeleteAddressGroupParams{ID: id, Expected: expected(expect)})
	if isPgCode(err, pgForeignKeyViolation) {
		return fmt.Errorf("%w: address group %s", policy.ErrInUse, id)
	}
	if err != nil {
		return fmt.Errorf("store: deleting address group: %w", err)
	}
	if n == 0 {
		row, err := s.q.GetAddressGroup(ctx, id)
		return versionMismatch(err, policy.ErrAddressGroupUnknown, row.Version)
	}
	return nil
}

// GetAddressGroup implements policy.Store.
func (s *Store) GetAddressGroup(ctx context.Context, id uuid.UUID) (*policy.AddressGroup, error) {
	row, err := s.q.GetAddressGroup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, policy.ErrAddressGroupUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up address group: %w", err)
	}
	cidrs, err := s.q.ListAddressGroupCIDRs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("store: listing address group cidrs: %w", err)
	}
	g := &policy.AddressGroup{ID: row.ID, Name: row.Name, CIDRs: make([]string, 0, len(cidrs)), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Version: row.Version}
	for _, c := range cidrs {
		g.CIDRs = append(g.CIDRs, c.Cidr)
	}
	return g, nil
}

// ListAddressGroups implements policy.Store.
func (s *Store) ListAddressGroups(ctx context.Context) ([]policy.AddressGroup, error) {
	return listAddressGroups(ctx, s.q)
}

func listAddressGroups(ctx context.Context, q *db.Queries) ([]policy.AddressGroup, error) {
	rows, err := q.ListAddressGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing address groups: %w", err)
	}
	cidrs, err := q.ListAllAddressGroupCIDRs(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing address group cidrs: %w", err)
	}
	byGroup := map[uuid.UUID][]string{}
	for _, c := range cidrs {
		byGroup[c.AddressGroupID] = append(byGroup[c.AddressGroupID], c.Cidr)
	}
	out := make([]policy.AddressGroup, 0, len(rows))
	for _, r := range rows {
		out = append(out, policy.AddressGroup{ID: r.ID, Name: r.Name, CIDRs: byGroup[r.ID], CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version})
	}
	return out, nil
}

// --- rulesets --------------------------------------------------------------

// CreateRuleset implements policy.Store.
func (s *Store) CreateRuleset(ctx context.Context, rs *policy.Ruleset) error {
	return s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreateRuleset(ctx, db.CreateRulesetParams{ID: rs.ID, Name: rs.Name, Description: rs.Description, Enabled: rs.Enabled, CreatedAt: rs.CreatedAt}); err != nil {
			if isPgCode(err, pgUniqueViolation) {
				return fmt.Errorf("%w: ruleset %q", policy.ErrDuplicateName, rs.Name)
			}
			return fmt.Errorf("store: creating ruleset: %w", err)
		}
		return addRulesetChildren(ctx, q, rs)
	})
}

// UpdateRuleset implements policy.Store.
func (s *Store) UpdateRuleset(ctx context.Context, rs *policy.Ruleset, expect string) error {
	return s.tx(ctx, func(q *db.Queries) error {
		version, err := q.UpdateRuleset(ctx, db.UpdateRulesetParams{ID: rs.ID, Name: rs.Name, Description: rs.Description, Enabled: rs.Enabled, UpdatedAt: rs.UpdatedAt, Expected: expected(expect)})
		if errors.Is(err, pgx.ErrNoRows) {
			row, err := q.GetRuleset(ctx, rs.ID)
			return versionMismatch(err, policy.ErrRulesetUnknown, row.Version)
		}
		if err != nil {
			if isPgCode(err, pgUniqueViolation) {
				return fmt.Errorf("%w: ruleset %q", policy.ErrDuplicateName, rs.Name)
			}
			return fmt.Errorf("store: updating ruleset: %w", err)
		}
		rs.Version = version
		if err := q.DeleteRulesetScopeMatches(ctx, rs.ID); err != nil {
			return fmt.Errorf("store: replacing ruleset scope: %w", err)
		}
		if err := q.DeleteRules(ctx, rs.ID); err != nil {
			return fmt.Errorf("store: replacing rules: %w", err)
		}
		return addRulesetChildren(ctx, q, rs)
	})
}

func addRulesetChildren(ctx context.Context, q *db.Queries, rs *policy.Ruleset) error {
	for _, key := range sortedKeys(rs.Scope) {
		if err := q.AddRulesetScopeMatch(ctx, db.AddRulesetScopeMatchParams{RulesetID: rs.ID, Key: key, Values: rs.Scope[key]}); err != nil {
			return fmt.Errorf("store: adding scope match: %w", err)
		}
	}
	for i := range rs.Rules {
		r := &rs.Rules[i]
		if err := q.AddRule(ctx, db.AddRuleParams{ID: r.ID, RulesetID: rs.ID, Ordinal: int32(i), Direction: int32(r.Direction), Enabled: r.Enabled, Description: r.Description, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version}); err != nil { //nolint:gosec // small counts
			if isPgCode(err, pgUniqueViolation) {
				return fmt.Errorf("%w: rule %s", policy.ErrDuplicateRuleID, r.ID)
			}
			return fmt.Errorf("store: adding rule: %w", err)
		}
		for j := range r.Peers {
			p := &r.Peers[j]
			params := db.AddRulePeerParams{RuleID: r.ID, Ordinal: int32(j), Kind: int32(p.Kind)} //nolint:gosec // small counts
			switch p.Kind {
			case policy.PeerAddressGroup:
				id := p.AddressGroupID
				params.AddressGroupID = &id
			case policy.PeerCIDR:
				cidr := p.CIDR
				params.Cidr = &cidr
			case policy.PeerWorkloads, policy.PeerUnspecified:
			}
			if err := q.AddRulePeer(ctx, params); err != nil {
				if isPgCode(err, pgForeignKeyViolation) {
					return fmt.Errorf("%w: address group %s", policy.ErrUnknownAddressGroup, p.AddressGroupID)
				}
				return fmt.Errorf("store: adding rule peer: %w", err)
			}
			if p.Kind == policy.PeerWorkloads {
				for _, key := range sortedKeys(p.Workloads) {
					if err := q.AddRulePeerMatch(ctx, db.AddRulePeerMatchParams{RuleID: r.ID, PeerOrdinal: int32(j), Key: key, Values: p.Workloads[key]}); err != nil { //nolint:gosec // small counts
						return fmt.Errorf("store: adding peer match: %w", err)
					}
				}
			}
		}
		for _, sid := range r.ServiceIDs {
			if err := q.AddRuleServiceRef(ctx, db.AddRuleServiceRefParams{RuleID: r.ID, ServiceID: sid}); err != nil {
				if isPgCode(err, pgForeignKeyViolation) {
					return fmt.Errorf("%w: service %s", policy.ErrUnknownService, sid)
				}
				return fmt.Errorf("store: adding service reference: %w", err)
			}
		}
		ordinal := int32(0)
		for _, e := range r.Entries {
			for _, row := range entryRows(e) {
				if err := q.AddRuleServiceEntry(ctx, db.AddRuleServiceEntryParams{RuleID: r.ID, Ordinal: ordinal, Protocol: int32(e.Protocol), PortStart: row[0], PortEnd: row[1]}); err != nil {
					return fmt.Errorf("store: adding rule entry: %w", err)
				}
				ordinal++
			}
		}
	}
	return nil
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DeleteRuleset implements policy.Store.
func (s *Store) DeleteRuleset(ctx context.Context, id uuid.UUID, expect string) error {
	n, err := s.q.DeleteRuleset(ctx, db.DeleteRulesetParams{ID: id, Expected: expected(expect)})
	if err != nil {
		return fmt.Errorf("store: deleting ruleset: %w", err)
	}
	if n == 0 {
		row, err := s.q.GetRuleset(ctx, id)
		return versionMismatch(err, policy.ErrRulesetUnknown, row.Version)
	}
	return nil
}

// GetRuleset implements policy.Store.
func (s *Store) GetRuleset(ctx context.Context, id uuid.UUID) (*policy.Ruleset, error) {
	if _, err := s.q.GetRuleset(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return nil, policy.ErrRulesetUnknown
	} else if err != nil {
		return nil, fmt.Errorf("store: looking up ruleset: %w", err)
	}
	all, err := listRulesets(ctx, s.q)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, policy.ErrRulesetUnknown
}

// ListRulesets implements policy.Store.
func (s *Store) ListRulesets(ctx context.Context) ([]policy.Ruleset, error) {
	return listRulesets(ctx, s.q)
}

// listRulesets loads every ruleset with its children in a fixed number of
// queries, which is also what a render needs.
func listRulesets(ctx context.Context, q *db.Queries) ([]policy.Ruleset, error) {
	rows, err := q.ListRulesets(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing rulesets: %w", err)
	}
	scopes, err := q.ListAllRulesetScopeMatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing scopes: %w", err)
	}
	rules, err := q.ListAllRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing rules: %w", err)
	}
	peers, err := q.ListAllRulePeers(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing peers: %w", err)
	}
	matches, err := q.ListAllRulePeerMatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing peer matches: %w", err)
	}
	refs, err := q.ListAllRuleServiceRefs(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing service references: %w", err)
	}
	entries, err := q.ListAllRuleServiceEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing rule entries: %w", err)
	}

	matchIndex := map[uuid.UUID]map[int32]policy.Selector{}
	for _, m := range matches {
		if matchIndex[m.RuleID] == nil {
			matchIndex[m.RuleID] = map[int32]policy.Selector{}
		}
		if matchIndex[m.RuleID][m.PeerOrdinal] == nil {
			matchIndex[m.RuleID][m.PeerOrdinal] = policy.Selector{}
		}
		matchIndex[m.RuleID][m.PeerOrdinal][m.Key] = m.Values
	}
	peerIndex := map[uuid.UUID][]policy.Peer{}
	for _, p := range peers {
		peer := policy.Peer{Kind: policy.PeerKind(p.Kind)}
		switch peer.Kind {
		case policy.PeerWorkloads:
			peer.Workloads = matchIndex[p.RuleID][p.Ordinal]
			if peer.Workloads == nil {
				peer.Workloads = policy.Selector{}
			}
		case policy.PeerAddressGroup:
			if p.AddressGroupID != nil {
				peer.AddressGroupID = *p.AddressGroupID
			}
		case policy.PeerCIDR:
			if p.Cidr != nil {
				peer.CIDR = *p.Cidr
			}
		case policy.PeerUnspecified:
		}
		peerIndex[p.RuleID] = append(peerIndex[p.RuleID], peer)
	}
	refIndex := map[uuid.UUID][]uuid.UUID{}
	for _, r := range refs {
		refIndex[r.RuleID] = append(refIndex[r.RuleID], r.ServiceID)
	}
	entryIndex := map[uuid.UUID][]flatEntry{}
	for _, e := range entries {
		entryIndex[e.RuleID] = append(entryIndex[e.RuleID], flatEntry{e.Protocol, e.PortStart, e.PortEnd})
	}
	ruleIndex := map[uuid.UUID][]policy.Rule{}
	for _, r := range rules {
		ruleIndex[r.RulesetID] = append(ruleIndex[r.RulesetID], policy.Rule{
			ID:          r.ID,
			Direction:   innerwallv1.Direction(r.Direction),
			Enabled:     r.Enabled,
			Description: r.Description,
			Peers:       peerIndex[r.ID],
			ServiceIDs:  refIndex[r.ID],
			Entries:     entriesFromRows(entryIndex[r.ID]),
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
			Version:     r.Version,
		})
	}
	scopeIndex := map[uuid.UUID]policy.Selector{}
	for _, sm := range scopes {
		if scopeIndex[sm.RulesetID] == nil {
			scopeIndex[sm.RulesetID] = policy.Selector{}
		}
		scopeIndex[sm.RulesetID][sm.Key] = sm.Values
	}
	out := make([]policy.Ruleset, 0, len(rows))
	for _, r := range rows {
		scope := scopeIndex[r.ID]
		if scope == nil {
			scope = policy.Selector{}
		}
		out = append(out, policy.Ruleset{
			ID: r.ID, Name: r.Name, Description: r.Description, Enabled: r.Enabled,
			Scope: scope, Rules: ruleIndex[r.ID], CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version,
		})
	}
	return out, nil
}
