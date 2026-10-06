package readmodel

import (
	"context"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
)

// DefaultGapLimit and MaxGapLimit bound the evidence gaps one read
// returns.
const (
	DefaultGapLimit = 500
	MaxGapLimit     = 5000
)

// GapsRequest asks for the evidence gaps of a workload set that intersect
// a range. The range, Workload, and Selector follow RollupRequest: a nil
// Workload and empty Selector cover every workload, and a Selector is
// resolved to the workloads it currently matches. Limit bounds the gaps;
// DefaultGapLimit when zero, MaxGapLimit at most.
type GapsRequest struct {
	From     time.Time
	To       time.Time
	Workload *identity.WorkloadID
	Selector policy.Selector
	Limit    int
}

// Gap is one interval [From, To) in which a workload's flow evidence is
// known to be incomplete (ADR-0019 as amended). Count is the records or
// table entries lost, nil when unknown.
type Gap struct {
	Workload WorkloadRef
	Kind     innerwallv1.EvidenceGapKind
	Source   innerwallv1.EvidenceSource
	From     time.Time
	To       time.Time
	Count    *uint64
}

// Gaps is the result of a gaps read: the range asked, the gaps
// intersecting it newest first, and whether more exist beyond the limit.
type Gaps struct {
	Range     Range
	Gaps      []Gap
	Truncated bool
}

// Gaps reads the evidence gaps of a workload set intersecting a range,
// through the store's one named statement.
func (s *Reader) Gaps(ctx context.Context, req GapsRequest) (*Gaps, error) {
	r, err := s.resolveRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	out := &Gaps{Range: r, Gaps: []Gap{}}
	ids, scoped, err := s.scope(ctx, req.Workload, req.Selector)
	if err != nil {
		return nil, err
	}
	if scoped && len(ids) == 0 {
		return out, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultGapLimit
	}
	limit = min(limit, MaxGapLimit)
	rows, err := s.Flows.ListGaps(ctx, flowstore.GapQuery{WorkloadIDs: ids, Since: r.From, Until: r.To, Limit: limit + 1})
	if err != nil {
		return nil, err
	}
	if len(rows) > limit {
		rows, out.Truncated = rows[:limit], true
	}
	if len(rows) == 0 {
		return out, nil
	}
	names, err := s.loadNames(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		g := &rows[i]
		out.Gaps = append(out.Gaps, Gap{Workload: names.workload(g.WorkloadID), Kind: g.Kind, Source: g.Source, From: g.From.UTC(), To: g.To.UTC(), Count: g.Count})
	}
	return out, nil
}

// GapKindName is a gap kind as the surface and the command line name it.
func GapKindName(k innerwallv1.EvidenceGapKind) string {
	switch k {
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_OVERRUN:
		return "source_overrun"
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_RESTART:
		return "source_restart"
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_BUFFER_OVERFLOW:
		return "buffer_overflow"
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_DUMP_TRUNCATED:
		return "dump_truncated"
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_WINDOW_OVERFLOW:
		return "window_overflow"
	case innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_UNSPECIFIED:
		return "unknown"
	default:
		return "unknown"
	}
}

// GapSourceName is a gap's source as the surface and the command line
// name it; empty for a loss that is not one source's.
func GapSourceName(s innerwallv1.EvidenceSource) string {
	switch s {
	case innerwallv1.EvidenceSource_EVIDENCE_SOURCE_CONNTRACK:
		return "conntrack"
	case innerwallv1.EvidenceSource_EVIDENCE_SOURCE_NFLOG:
		return "nflog"
	case innerwallv1.EvidenceSource_EVIDENCE_SOURCE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}
