package flowstore

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

// RollupGroups implements FlowStore. Each grouping is its own statement;
// the query's grouping chooses which one runs and nothing is assembled.
func (p *Postgres) RollupGroups(ctx context.Context, q GroupQuery) (*GroupResult, error) {
	ids := make([]uuid.UUID, 0, len(q.WorkloadIDs))
	for _, id := range q.WorkloadIDs {
		ids = append(ids, id.UUID())
	}
	order := string(q.Order)
	if q.Order == "" {
		order = string(OrderByConnections)
	}
	limit := int32(boundedGroupLimit(q.Limit)) //nolint:gosec // bounded above
	f := struct {
		since, until        time.Time
		decision, direction int32
		protocol, port      int32
	}{q.Since, q.Until, int32(q.Decision), int32(q.Direction), int32(q.Protocol), int32(q.DstPort)}

	out := &GroupResult{Groups: []Group{}}
	switch q.GroupBy {
	case GroupByRule:
		rows, err := p.q.RollupFlowsByRule(ctx, db.RollupFlowsByRuleParams{WorkloadIds: ids, Since: f.since, Until: f.until, Decision: f.decision, Direction: f.direction, Protocol: f.protocol, DstPort: f.port, OrderBy: order, GroupLimit: limit})
		if err != nil {
			return nil, fmt.Errorf("flowstore: rolling up by rule: %w", err)
		}
		for i := range rows {
			r := &rows[i]
			out.Groups = append(out.Groups, Group{RuleID: r.MatchedRuleID, FlowCount: r.FlowCount, ConnectionCount: unsigned(r.ConnectionCount), ByteCount: unsigned(r.ByteCount), FirstSeen: r.FirstSeen, LastSeen: r.LastSeen})
			if i == 0 {
				out.EffectiveFrom, out.EffectiveTo, out.GroupCount, out.FlowCount, out.ConnectionCount, out.ByteCount = r.EffectiveFrom, r.EffectiveTo, r.GroupCount, r.TotalFlowCount, unsigned(r.TotalConnectionCount), unsigned(r.TotalByteCount)
			}
		}
	case GroupByRulePeer:
		rows, err := p.q.RollupFlowsByRulePeer(ctx, db.RollupFlowsByRulePeerParams{WorkloadIds: ids, Since: f.since, Until: f.until, Decision: f.decision, Direction: f.direction, Protocol: f.protocol, DstPort: f.port, OrderBy: order, GroupLimit: limit})
		if err != nil {
			return nil, fmt.Errorf("flowstore: rolling up by rule and peer: %w", err)
		}
		for i := range rows {
			r := &rows[i]
			out.Groups = append(out.Groups, Group{RuleID: r.MatchedRuleID, Peer: Peer{Kind: PeerKind(r.PeerKind), Key: r.PeerKey, Labels: decodeLabels(r.PeerLabels)}, FlowCount: r.FlowCount, ConnectionCount: unsigned(r.ConnectionCount), ByteCount: unsigned(r.ByteCount), FirstSeen: r.FirstSeen, LastSeen: r.LastSeen})
			if i == 0 {
				out.EffectiveFrom, out.EffectiveTo, out.GroupCount, out.FlowCount, out.ConnectionCount, out.ByteCount = r.EffectiveFrom, r.EffectiveTo, r.GroupCount, r.TotalFlowCount, unsigned(r.TotalConnectionCount), unsigned(r.TotalByteCount)
			}
		}
	case GroupBySrcDst:
		rows, err := p.q.RollupFlowsBySrcDst(ctx, db.RollupFlowsBySrcDstParams{WorkloadIds: ids, Since: f.since, Until: f.until, Decision: f.decision, Direction: f.direction, Protocol: f.protocol, DstPort: f.port, OrderBy: order, GroupLimit: limit})
		if err != nil {
			return nil, fmt.Errorf("flowstore: rolling up by source and destination: %w", err)
		}
		for i := range rows {
			r := &rows[i]
			out.Groups = append(out.Groups, Group{Peer: Peer{Kind: PeerKind(r.PeerKind), Key: r.PeerKey, Labels: decodeLabels(r.PeerLabels)}, WorkloadID: identity.FromUUID(r.WorkloadID), FlowCount: r.FlowCount, ConnectionCount: unsigned(r.ConnectionCount), ByteCount: unsigned(r.ByteCount), FirstSeen: r.FirstSeen, LastSeen: r.LastSeen})
			if i == 0 {
				out.EffectiveFrom, out.EffectiveTo, out.GroupCount, out.FlowCount, out.ConnectionCount, out.ByteCount = r.EffectiveFrom, r.EffectiveTo, r.GroupCount, r.TotalFlowCount, unsigned(r.TotalConnectionCount), unsigned(r.TotalByteCount)
			}
		}
	case GroupByDstService:
		rows, err := p.q.RollupFlowsByDstService(ctx, db.RollupFlowsByDstServiceParams{WorkloadIds: ids, Since: f.since, Until: f.until, Decision: f.decision, Direction: f.direction, Protocol: f.protocol, DstPort: f.port, OrderBy: order, GroupLimit: limit})
		if err != nil {
			return nil, fmt.Errorf("flowstore: rolling up by destination and service: %w", err)
		}
		for i := range rows {
			r := &rows[i]
			out.Groups = append(out.Groups, Group{WorkloadID: identity.FromUUID(r.WorkloadID), DstPort: uint16(r.DstPort), Protocol: innerwallv1.Protocol(r.Protocol), FlowCount: r.FlowCount, ConnectionCount: unsigned(r.ConnectionCount), ByteCount: unsigned(r.ByteCount), FirstSeen: r.FirstSeen, LastSeen: r.LastSeen}) //nolint:gosec // checked by the schema
			if i == 0 {
				out.EffectiveFrom, out.EffectiveTo, out.GroupCount, out.FlowCount, out.ConnectionCount, out.ByteCount = r.EffectiveFrom, r.EffectiveTo, r.GroupCount, r.TotalFlowCount, unsigned(r.TotalConnectionCount), unsigned(r.TotalByteCount)
			}
		}
	default:
		return nil, fmt.Errorf("%w %q", ErrUnknownGroupBy, q.GroupBy)
	}
	return out, nil
}

// ListWindowPage implements FlowStore.
func (p *Postgres) ListWindowPage(ctx context.Context, q WindowPageQuery) ([]WindowRow, error) {
	// The first page continues after a position beyond any row.
	cursorStart, cursorID := time.Unix(math.MaxInt32, 0).UTC(), int64(math.MaxInt64)
	if q.Before != nil {
		cursorStart, cursorID = q.Before.WindowStart, q.Before.ID
	}
	rows, err := p.q.ListFlowWindowPage(ctx, db.ListFlowWindowPageParams{
		WorkloadID: q.WorkloadID.UUID(), Since: q.Since, Until: q.Until,
		Decision: int32(q.Decision), Direction: int32(q.Direction), PeerKey: q.PeerKey,
		Protocol: int32(q.Protocol), DstPort: int32(q.DstPort),
		CursorStart: cursorStart, CursorID: cursorID,
		RowLimit: int32(boundedPageLimit(q.Limit)), //nolint:gosec // bounded above
	})
	if err != nil {
		return nil, fmt.Errorf("flowstore: listing window page: %w", err)
	}
	out := make([]WindowRow, 0, len(rows))
	for i := range rows {
		out = append(out, windowRowFromDB(&rows[i]))
	}
	return out, nil
}

// unsigned converts a stored counter, which is non-negative by
// construction.
func unsigned(n int64) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}
