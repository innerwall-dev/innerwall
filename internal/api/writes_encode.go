package api

import (
	"errors"
	"fmt"
	"time"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// The JSON shapes of the write endpoints. Rulesets and rules travel in
// the domain's document form, the same one the command line reads and
// writes: on the way in a service or address group may be named by id
// or by name; on the way out the surface writes ids, since a name can
// change under a client between its read and its write. Every object
// carries its version, which is also its entity tag.

// --- rulesets and rules ------------------------------------------------------

type rulesetsResponse struct {
	Rulesets     []*policy.RulesetDoc `json:"rulesets"`
	StateVersion string               `json:"state_version"`
}

// rulesetJSON is the document form with ids for every reference.
func rulesetJSON(rs *policy.Ruleset) *policy.RulesetDoc {
	doc := policy.RulesetToDoc(rs, policy.Names{})
	if doc.Scope == nil {
		doc.Scope = map[string][]string{}
	}
	return doc
}

func authoredRuleJSON(r *policy.Rule) *policy.RuleDoc {
	return policy.RuleToDoc(r, policy.Names{})
}

// --- services ----------------------------------------------------------------

type serviceInput struct {
	Name    string            `json:"name"`
	Entries []policy.EntryDoc `json:"entries"`
}

// service converts the input, reporting entry faults as findings.
func (in *serviceInput) service() (*policy.Service, error) {
	svc := &policy.Service{Name: in.Name}
	f := &policy.Findings{}
	for i, e := range in.Entries {
		entry, err := policy.EntryFromDoc(e)
		if err != nil {
			for _, fe := range policy.AsFindings(err).Errors {
				fe.Path = fmt.Sprintf("entries[%d].%s", i, fe.Path)
				f.Errors = append(f.Errors, fe)
			}
			continue
		}
		svc.Entries = append(svc.Entries, entry)
	}
	if len(f.Errors) > 0 {
		return nil, f
	}
	return svc, nil
}

type serviceDocJSON struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Entries   []policy.EntryDoc `json:"entries"`
	Version   string            `json:"version"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

func serviceJSONOf(s *policy.Service) serviceDocJSON {
	out := serviceDocJSON{ID: s.ID.String(), Name: s.Name, Entries: make([]policy.EntryDoc, 0, len(s.Entries)), Version: policy.FormatVersion(s.Version), CreatedAt: timestamp(s.CreatedAt), UpdatedAt: timestamp(s.UpdatedAt)}
	for _, e := range s.Entries {
		out.Entries = append(out.Entries, policy.EntryToDoc(e))
	}
	return out
}

type servicesResponse struct {
	Services []serviceDocJSON `json:"services"`
}

// --- address groups ----------------------------------------------------------

type addressGroupInput struct {
	Name  string   `json:"name"`
	CIDRs []string `json:"cidrs"`
}

func (in *addressGroupInput) group() *policy.AddressGroup {
	return &policy.AddressGroup{Name: in.Name, CIDRs: in.CIDRs}
}

type addressGroupJSON struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	CIDRs     []string `json:"cidrs"`
	Version   string   `json:"version"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func addressGroupJSONOf(g *policy.AddressGroup) addressGroupJSON {
	out := addressGroupJSON{ID: g.ID.String(), Name: g.Name, CIDRs: g.CIDRs, Version: policy.FormatVersion(g.Version), CreatedAt: timestamp(g.CreatedAt), UpdatedAt: timestamp(g.UpdatedAt)}
	if out.CIDRs == nil {
		out.CIDRs = []string{}
	}
	return out
}

type addressGroupsResponse struct {
	AddressGroups []addressGroupJSON `json:"address_groups"`
}

// --- labels ------------------------------------------------------------------

type labelsInput struct {
	Labels map[string]string `json:"labels"`
}

func (in *labelsInput) labels() []registry.Label {
	out := make([]registry.Label, 0, len(in.Labels))
	for _, k := range sortedKeys(in.Labels) {
		out = append(out, registry.Label{Key: k, Value: in.Labels[k]})
	}
	return out
}

type labelsResponse struct {
	Labels  map[string]string `json:"labels"`
	Version string            `json:"version"`
}

// --- mode changes ------------------------------------------------------------

type modeChangeInput struct {
	Selector           map[string][]string `json:"selector"`
	WorkloadIDs        []string            `json:"workload_ids"`
	TargetMode         string              `json:"target_mode"`
	ExpectedMatchCount *int                `json:"expected_match_count"`
}

// request converts the input. A value that cannot be read into the
// domain's type (an id that is not an id) is a validation problem at its
// path; everything about the request's meaning is judged by the domain.
func (in *modeChangeInput) request() (fleet.ModeChangeRequest, *Problem) {
	req := fleet.ModeChangeRequest{Selector: policy.Selector(in.Selector)}
	for i, s := range in.WorkloadIDs {
		id, err := identity.ParseWorkloadID(s)
		if err != nil {
			p := finding(fmt.Sprintf("workload_ids[%d]", i), "id", "not a workload id")
			return req, &p
		}
		req.WorkloadIDs = append(req.WorkloadIDs, id)
	}
	if in.TargetMode != "" {
		// An unknown name is left unspecified for the domain to refuse
		// with its own finding.
		req.TargetMode, _ = policy.ParseMode(in.TargetMode)
	}
	if in.ExpectedMatchCount != nil {
		req.ExpectedMatchCount = *in.ExpectedMatchCount
	}
	return req, nil
}

type modeChangeResponse struct {
	ID             string `json:"mode_change_id"`
	Matched        int    `json:"matched"`
	DesiredUpdated int    `json:"desired_updated"`
}

// --- selector preview --------------------------------------------------------

type selectorInput struct {
	Selector map[string][]string `json:"selector"`
}

type previewResponse struct {
	Matched []workloadRefJSON `json:"matched"`
	Count   int               `json:"count"`
}

func previewJSON(p *fleet.Preview) previewResponse {
	out := previewResponse{Matched: make([]workloadRefJSON, 0, len(p.Matched)), Count: len(p.Matched)}
	for _, m := range p.Matched {
		labels := m.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		out.Matched = append(out.Matched, workloadRefJSON{ID: m.ID.String(), Hostname: m.Hostname, Labels: labels})
	}
	return out
}

// --- dry run -----------------------------------------------------------------

type dryRunInput struct {
	StateVersion string              `json:"state_version"`
	Rulesets     []policy.RulesetDoc `json:"rulesets"`
}

// request converts every ruleset document, collecting document faults
// under rulesets[i].
func (in *dryRunInput) request(names policy.Names) (fleet.DryRunRequest, error) {
	req := fleet.DryRunRequest{StateVersion: in.StateVersion}
	f := &policy.Findings{}
	for i := range in.Rulesets {
		rs, err := policy.RulesetFromDoc(&in.Rulesets[i], names)
		if err != nil {
			for _, fe := range policy.AsFindings(err).Errors {
				fe.Path = fmt.Sprintf("rulesets[%d].%s", i, fe.Path)
				f.Errors = append(f.Errors, fe)
			}
			continue
		}
		req.Rulesets = append(req.Rulesets, *rs)
	}
	if len(f.Errors) > 0 {
		return req, f
	}
	return req, nil
}

type renderedRuleDeltaJSON struct {
	ID        string          `json:"id"`
	Protocol  string          `json:"protocol"`
	Ports     []portRangeJSON `json:"ports"`
	PeerCIDRs []string        `json:"peer_cidrs"`
}

func renderedRuleDeltaJSONOf(r *innerwallv1.ResolvedRule) renderedRuleDeltaJSON {
	out := renderedRuleDeltaJSON{ID: r.GetRuleId(), Protocol: policy.ProtocolName(r.GetProtocol()), Ports: make([]portRangeJSON, 0, len(r.GetPorts())), PeerCIDRs: append([]string{}, r.GetPeerCidrs()...)}
	for _, p := range r.GetPorts() {
		out.Ports = append(out.Ports, portRangeJSON{Start: p.GetStart(), End: p.GetEnd()})
	}
	return out
}

type ruleChangeJSON struct {
	Before renderedRuleDeltaJSON `json:"before"`
	After  renderedRuleDeltaJSON `json:"after"`
}

type modeDeltaJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type workloadDiffJSON struct {
	Workload workloadRefJSON         `json:"workload"`
	Version  uint64                  `json:"version"`
	Mode     *modeDeltaJSON          `json:"mode"`
	Added    []renderedRuleDeltaJSON `json:"added"`
	Removed  []renderedRuleDeltaJSON `json:"removed"`
	Changed  []ruleChangeJSON        `json:"changed"`
}

type dryRunResponse struct {
	StateVersion string             `json:"state_version"`
	Stale        bool               `json:"stale"`
	Workloads    []workloadDiffJSON `json:"workloads"`
}

func dryRunJSON(res *fleet.DryRunResult, workloads []registry.Workload) dryRunResponse {
	byID := make(map[identity.WorkloadID]*registry.Workload, len(workloads))
	for i := range workloads {
		byID[workloads[i].ID] = &workloads[i]
	}
	out := dryRunResponse{StateVersion: res.StateVersion, Stale: res.Stale, Workloads: make([]workloadDiffJSON, 0, len(res.Workloads))}
	for i := range res.Workloads {
		d := &res.Workloads[i]
		ref := workloadRefJSON{ID: d.ID.String(), Labels: map[string]string{}}
		if w, ok := byID[d.ID]; ok {
			ref.Hostname, ref.Labels = w.Hostname, w.LabelMap()
		}
		wd := workloadDiffJSON{Workload: ref, Version: d.Version, Added: make([]renderedRuleDeltaJSON, 0, len(d.Added)), Removed: make([]renderedRuleDeltaJSON, 0, len(d.Removed)), Changed: make([]ruleChangeJSON, 0, len(d.Changed))}
		if d.Mode != nil {
			wd.Mode = &modeDeltaJSON{From: policy.ModeName(d.Mode.From), To: policy.ModeName(d.Mode.To)}
		}
		for _, r := range d.Added {
			wd.Added = append(wd.Added, renderedRuleDeltaJSONOf(r))
		}
		for _, r := range d.Removed {
			wd.Removed = append(wd.Removed, renderedRuleDeltaJSONOf(r))
		}
		for _, c := range d.Changed {
			wd.Changed = append(wd.Changed, ruleChangeJSON{Before: renderedRuleDeltaJSONOf(c.Before), After: renderedRuleDeltaJSONOf(c.After)})
		}
		out.Workloads = append(out.Workloads, wd)
	}
	return out
}

// --- tokens ------------------------------------------------------------------

// tokenState names a token's state at an instant, from the same check
// enrollment and authentication apply.
func tokenState(err error, revoked, expired error) string {
	switch {
	case err == nil:
		return "valid"
	case errors.Is(err, revoked):
		return "revoked"
	case errors.Is(err, expired):
		return "expired"
	default:
		return "invalid"
	}
}

type provisioningTokenInput struct {
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels"`
	TTLSeconds int64             `json:"ttl_seconds"`
}

func (in *provisioningTokenInput) ttl() time.Duration {
	return time.Duration(in.TTLSeconds) * time.Second
}

type provisioningTokenJSON struct {
	ID string `json:"id"`
	// Prefix is the listing hint; null for a token minted before hints
	// were kept.
	Prefix     *string           `json:"prefix"`
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels"`
	State      string            `json:"state"`
	CreatedAt  string            `json:"created_at"`
	ExpiresAt  string            `json:"expires_at"`
	RevokedAt  *string           `json:"revoked_at"`
	UseCount   int64             `json:"use_count"`
	LastUsedAt *string           `json:"last_used_at"`
}

func provisioningTokenJSONOf(t *enroll.Token, now time.Time) provisioningTokenJSON {
	labels := make(map[string]string, len(t.Labels))
	for _, l := range t.Labels {
		labels[l.Key] = l.Value
	}
	return provisioningTokenJSON{
		ID: t.ID.String(), Prefix: t.Prefix, Name: t.Name, Labels: labels, State: tokenState(t.Check(now), enroll.ErrTokenRevoked, enroll.ErrTokenExpired),
		CreatedAt: timestamp(t.CreatedAt), ExpiresAt: timestamp(t.ExpiresAt), RevokedAt: optionalTimestamp(t.RevokedAt), UseCount: t.UseCount, LastUsedAt: optionalTimestamp(t.LastUsedAt),
	}
}

// mintedProvisioningTokenJSON is the mint response: the metadata plus the
// secret, shown here once.
type mintedProvisioningTokenJSON struct {
	provisioningTokenJSON
	Token string `json:"token"`
}

type provisioningTokensResponse struct {
	Tokens []provisioningTokenJSON `json:"tokens"`
}

type operatorTokenInput struct {
	Name       string `json:"name"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

func (in *operatorTokenInput) ttl() time.Duration { return time.Duration(in.TTLSeconds) * time.Second }

type operatorTokenJSON struct {
	ID         string  `json:"id"`
	Prefix     string  `json:"prefix"`
	Name       string  `json:"name"`
	State      string  `json:"state"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  *string `json:"expires_at"`
	RevokedAt  *string `json:"revoked_at"`
	LastUsedAt *string `json:"last_used_at"`
}

func operatorTokenJSONOf(t *operator.Token, now time.Time) operatorTokenJSON {
	return operatorTokenJSON{
		ID: t.ID.String(), Prefix: t.Prefix, Name: t.Name, State: tokenState(t.Check(now), operator.ErrTokenRevoked, operator.ErrTokenExpired),
		CreatedAt: timestamp(t.CreatedAt), ExpiresAt: optionalTimestamp(t.ExpiresAt), RevokedAt: optionalTimestamp(t.RevokedAt), LastUsedAt: optionalTimestamp(t.LastUsedAt),
	}
}

type mintedOperatorTokenJSON struct {
	operatorTokenJSON
	Token string `json:"token"`
}

type operatorTokensResponse struct {
	Tokens []operatorTokenJSON `json:"tokens"`
}

// resendSnapshotResponse acknowledges a directed reconnect with the
// workload's snapshot instant as it stood when the directive was fired.
type resendSnapshotResponse struct {
	LastSnapshotSentAt *string `json:"last_snapshot_sent_at"`
}
