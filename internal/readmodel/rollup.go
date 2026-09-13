package readmodel

import (
	"context"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
)

// RollupRequest asks for one grouped rollup. A zero From or To takes the
// default range; a zero Verdict or Direction means any; a nil Workload
// and empty Selector cover every workload, a Workload restricts to that
// one, a Selector to the workloads it currently matches (ADR-0019
// resolves a scope at read time, deliberately), and both to their
// intersection; a nil Service means every service.
type RollupRequest struct {
	From      time.Time
	To        time.Time
	GroupBy   flowstore.GroupBy
	Verdict   innerwallv1.PolicyDecision
	Direction innerwallv1.Direction
	Workload  *identity.WorkloadID
	Selector  policy.Selector
	Service   *Service
	Order     flowstore.GroupOrder
	// Limit bounds the groups; the store's default and maximum apply.
	Limit int
}

// Counters are the summed counts of a group or of a whole rollup.
type Counters struct {
	// FlowCount is the number of stored records.
	FlowCount       int64
	ConnectionCount uint64
	ByteCount       uint64
}

// GroupKeys are the dimensions of one group. Which are set follows the
// grouping: Rule for rule and rule,peer (nil for the group of records no
// rule admitted); Peer for rule,peer; Src for src,dst; Dst for src,dst
// and dst,service; Service for dst,service.
type GroupKeys struct {
	Rule    *RuleRef
	Peer    *PeerRef
	Src     *PeerRef
	Dst     *WorkloadRef
	Service *Service
}

// RollupGroup is one group with its counters and the span it was seen
// over.
type RollupGroup struct {
	Keys GroupKeys
	Counters
	FirstSeen time.Time
	LastSeen  time.Time
}

// Rollup is a grouped rollup. Range is what was asked; EffectiveFrom and
// EffectiveTo are the bounds of the windows actually covered, since a
// range quantizes to window boundaries, and are nil when nothing
// matched. Groups are in the requested order; Truncated says groups
// beyond them exist, and GroupCount and Totals cover all of them.
type Rollup struct {
	Range         Range
	EffectiveFrom *time.Time
	EffectiveTo   *time.Time
	GroupBy       flowstore.GroupBy
	Groups        []RollupGroup
	GroupCount    int64
	Truncated     bool
	Totals        Counters
}

// Rollup runs one grouped rollup. The grouping is one of the store's
// named groupings and nothing else; the aggregation is the store's.
func (s *Reader) Rollup(ctx context.Context, req RollupRequest) (*Rollup, error) {
	if _, err := flowstore.ParseGroupBy(string(req.GroupBy)); err != nil {
		return nil, err
	}
	r, err := s.resolveRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	out := &Rollup{Range: r, GroupBy: req.GroupBy, Groups: []RollupGroup{}}
	ids, scoped, err := s.scope(ctx, req.Workload, req.Selector)
	if err != nil {
		return nil, err
	}
	if scoped && len(ids) == 0 {
		// A scope that matches no workload sees nothing; an empty id
		// list would mean every workload to the store.
		return out, nil
	}
	q := flowstore.GroupQuery{
		GroupBy: req.GroupBy, WorkloadIDs: ids, Since: r.From, Until: r.To,
		Decision: req.Verdict, Direction: req.Direction, Order: req.Order, Limit: req.Limit,
	}
	if req.Service != nil {
		q.Protocol, q.DstPort = req.Service.Protocol, req.Service.Port
	}
	res, err := s.Flows.RollupGroups(ctx, q)
	if err != nil {
		return nil, err
	}
	names, err := s.loadNames(ctx)
	if err != nil {
		return nil, err
	}
	if len(res.Groups) > 0 {
		from, to := res.EffectiveFrom, res.EffectiveTo
		out.EffectiveFrom, out.EffectiveTo = &from, &to
	}
	out.GroupCount = res.GroupCount
	out.Truncated = res.Truncated()
	out.Totals = Counters{FlowCount: res.FlowCount, ConnectionCount: res.ConnectionCount, ByteCount: res.ByteCount}
	for i := range res.Groups {
		g := &res.Groups[i]
		group := RollupGroup{
			Counters:  Counters{FlowCount: g.FlowCount, ConnectionCount: g.ConnectionCount, ByteCount: g.ByteCount},
			FirstSeen: g.FirstSeen, LastSeen: g.LastSeen,
		}
		switch req.GroupBy {
		case flowstore.GroupByRule:
			group.Keys.Rule = ruleRef(g.RuleID)
		case flowstore.GroupByRulePeer:
			peer := names.peer(g.Peer)
			group.Keys.Rule, group.Keys.Peer = ruleRef(g.RuleID), &peer
		case flowstore.GroupBySrcDst:
			src, dst := names.peer(g.Peer), names.workload(g.WorkloadID)
			group.Keys.Src, group.Keys.Dst = &src, &dst
		case flowstore.GroupByDstService:
			dst := names.workload(g.WorkloadID)
			group.Keys.Dst, group.Keys.Service = &dst, &Service{Protocol: g.Protocol, Port: g.DstPort}
		}
		out.Groups = append(out.Groups, group)
	}
	return out, nil
}
