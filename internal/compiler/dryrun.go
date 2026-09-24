package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// A dry run renders a hypothetical authored model against the persisted
// registry and reports, per workload, how the rendered policy would
// differ from the one persisted now. It is a pure function of its inputs:
// nothing is persisted, no version advances, and calling it twice with
// the same inputs says the same thing.

// RuleChange is one rendered rule the dry run would change: what the
// workload holds now and what it would hold.
type RuleChange struct {
	Before *innerwallv1.ResolvedRule
	After  *innerwallv1.ResolvedRule
}

// ModeChange is a mode the dry run would change.
type ModeChange struct {
	From innerwallv1.EnforcementMode
	To   innerwallv1.EnforcementMode
}

// WorkloadDiff is what a dry run would change on one workload. Only
// workloads whose rendered policy would differ are reported; a workload
// with no persisted policy is diffed against the empty one, as a first
// render would.
type WorkloadDiff struct {
	ID identity.WorkloadID
	// Version is the persisted version the diff is against; zero when no
	// policy has been rendered yet.
	Version uint64
	Mode    *ModeChange
	Added   []*innerwallv1.ResolvedRule
	Removed []*innerwallv1.ResolvedRule
	Changed []RuleChange
}

// DryRun diffs the freshly rendered policies (current) against the
// persisted ones (previous), classifying the wire contract's delta of
// each workload, computed by the one shared implementation (ADR-0018),
// into added, removed, and changed rules and a mode change. A rendered
// rule counts as changed when the delta upserts it over an existing one
// or adds or removes its peers. Workloads are reported in id order.
func DryRun(previous, current map[identity.WorkloadID]*innerwallv1.WorkloadPolicy) []WorkloadDiff {
	out := make([]WorkloadDiff, 0)
	for id, cur := range current {
		prev, ok := previous[id]
		if !ok {
			prev = rendered.Empty()
		}
		changes := rendered.Diff(prev, cur)
		if len(changes) == 0 {
			continue
		}
		prev, cur = rendered.Canonical(prev), rendered.Canonical(cur)
		before, after := indexRules(prev), indexRules(cur)
		d := WorkloadDiff{ID: id, Version: prev.GetVersion(), Added: []*innerwallv1.ResolvedRule{}, Removed: []*innerwallv1.ResolvedRule{}, Changed: []RuleChange{}}
		changed := map[string]bool{}
		for _, ch := range changes {
			switch c := ch.GetChange().(type) {
			case *innerwallv1.DeltaChange_UpsertRule:
				rid := c.UpsertRule.GetRuleId()
				if _, existed := before[rid]; existed {
					changed[rid] = true
				} else {
					d.Added = append(d.Added, after[rid])
				}
			case *innerwallv1.DeltaChange_RemoveRuleId:
				d.Removed = append(d.Removed, before[c.RemoveRuleId])
			case *innerwallv1.DeltaChange_AddPeers:
				changed[c.AddPeers.GetRuleId()] = true
			case *innerwallv1.DeltaChange_RemovePeers:
				changed[c.RemovePeers.GetRuleId()] = true
			case *innerwallv1.DeltaChange_SetMode:
				d.Mode = &ModeChange{From: prev.GetMode(), To: c.SetMode}
			}
		}
		ids := make([]string, 0, len(changed))
		for rid := range changed {
			ids = append(ids, rid)
		}
		sort.Strings(ids)
		for _, rid := range ids {
			d.Changed = append(d.Changed, RuleChange{Before: before[rid], After: after[rid]})
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out
}

func indexRules(p *innerwallv1.WorkloadPolicy) map[string]*innerwallv1.ResolvedRule {
	out := make(map[string]*innerwallv1.ResolvedRule, len(p.GetInboundRules()))
	for _, r := range p.GetInboundRules() {
		out[r.GetRuleId()] = r
	}
	return out
}

// StateVersion is the version of the state a render reads: a digest over
// everything in the inputs that can change a rendered policy (each
// workload's labels, addresses, and mode; each authored object's id and
// version). A caller that authored against one state version can tell
// from another that the persisted state has moved since, and a dry run
// reports the version it computed against so that staleness is
// detectable rather than silent. It is derived, never stored.
func StateVersion(in *Inputs) string {
	h := sha256.New()
	ws := make([]string, 0, len(in.Workloads))
	for i := range in.Workloads {
		w := &in.Workloads[i]
		labels := make([]string, 0, len(w.Labels))
		for _, l := range w.Labels {
			labels = append(labels, l.Key+"="+l.Value)
		}
		sort.Strings(labels)
		addrs := make([]string, 0, len(w.Addresses))
		for _, a := range w.Addresses {
			addrs = append(addrs, a.String())
		}
		sort.Strings(addrs)
		ws = append(ws, fmt.Sprintf("w %s %d %s %s", w.ID, w.Mode, strings.Join(labels, ","), strings.Join(addrs, ",")))
	}
	lines := ws
	for i := range in.Rulesets {
		lines = append(lines, fmt.Sprintf("r %s %d", in.Rulesets[i].ID, in.Rulesets[i].Version))
	}
	for i := range in.Services {
		lines = append(lines, fmt.Sprintf("s %s %d", in.Services[i].ID, in.Services[i].Version))
	}
	for i := range in.AddressGroups {
		lines = append(lines, fmt.Sprintf("g %s %d", in.AddressGroups[i].ID, in.AddressGroups[i].Version))
	}
	sort.Strings(lines)
	for _, l := range lines {
		_, _ = io.WriteString(h, l)
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
