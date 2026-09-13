package compiler

import (
	"sort"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// Outcome is Plan's verdict for one workload.
type Outcome struct {
	ID identity.WorkloadID
	// Policy is the rendered policy carrying the version it should be
	// persisted at: the previous version when nothing changed, one more
	// when something did.
	Policy *innerwallv1.WorkloadPolicy
	// Changes is the delta from the previously persisted policy. Empty
	// exactly when Changed is false.
	Changes []*innerwallv1.DeltaChange
	// Changed reports whether the version advanced: the rendered output
	// differs from what was persisted, or nothing was persisted yet.
	Changed bool
}

// Plan diffs each freshly rendered policy against the previously persisted
// one and assigns versions: unchanged output keeps its version, changed
// output takes the next. This is the whole of the versioning rule
// (ADR-0018): a version advances if and only if the rendered bytes differ,
// so a re-render of an unchanged estate is a no-op and an authored change
// that does not affect a workload leaves that workload's version alone.
// A workload with no persisted policy is planned against the empty policy
// at version 0 and always persisted at version 1, even when its rendered
// policy is itself empty, so that every registered workload has a durable
// snapshot from its first render on. Outcomes are sorted by workload id.
func Plan(previous, current map[identity.WorkloadID]*innerwallv1.WorkloadPolicy) []Outcome {
	out := make([]Outcome, 0, len(current))
	for id, cur := range current {
		prev, ok := previous[id]
		if !ok {
			prev = rendered.Empty()
		}
		changes := rendered.Diff(prev, cur)
		next := rendered.Canonical(cur)
		next.Version = prev.GetVersion()
		changed := len(changes) > 0 || !ok
		if changed {
			next.Version = prev.GetVersion() + 1
		}
		out = append(out, Outcome{ID: id, Policy: next, Changes: changes, Changed: changed})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out
}
