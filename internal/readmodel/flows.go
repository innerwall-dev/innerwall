package readmodel

import (
	"context"
	"net/netip"
	"strconv"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// FlowsRequest asks for one page of a workload's stored windows. Workload
// is required: there is no unbounded scan. A zero From or To takes the
// default range; a zero Verdict or Direction means any; an empty Peer
// means every peer, otherwise the stored peer key (a workload id, an
// address group id, or an address); a nil Service means every service.
// Cursor continues a previous page; empty is the first page.
type FlowsRequest struct {
	Workload  identity.WorkloadID
	From      time.Time
	To        time.Time
	Verdict   innerwallv1.PolicyDecision
	Direction innerwallv1.Direction
	Peer      string
	Service   *Service
	Cursor    string
	// Limit bounds the page; the store's default and maximum apply.
	Limit int
}

// Flow is one stored window row as the operator sees it.
type Flow struct {
	ID              int64
	WindowStart     time.Time
	WindowEnd       time.Time
	Peer            PeerRef
	SrcAddress      netip.Addr
	DstAddress      netip.Addr
	Service         Service
	Direction       innerwallv1.Direction
	Verdict         innerwallv1.PolicyDecision
	Rule            *RuleRef
	ConnectionCount uint64
	ByteCount       uint64
	FirstSeen       time.Time
	LastSeen        time.Time
	ProcessName     string
}

// FlowsPage is one page of flows, newest window first. NextCursor is
// empty on the last page.
type FlowsPage struct {
	Workload   WorkloadRef
	Range      Range
	Flows      []Flow
	NextCursor string
}

const flowsCursorKind = "f1"

func encodeFlowsCursor(c flowstore.WindowCursor) string {
	return encodeCursor(flowsCursorKind, strconv.FormatInt(c.WindowStart.UnixNano(), 10), strconv.FormatInt(c.ID, 10))
}

func decodeFlowsCursor(token string) (*flowstore.WindowCursor, error) {
	if token == "" {
		return nil, nil //nolint:nilnil // the first page has no cursor
	}
	parts, err := decodeCursor(token, flowsCursorKind, 2)
	if err != nil {
		return nil, err
	}
	start, err := cursorTime(parts[0])
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	return &flowstore.WindowCursor{WindowStart: start, ID: id}, nil
}

// ListFlows returns one page of a workload's windows.
func (s *Reader) ListFlows(ctx context.Context, req FlowsRequest) (*FlowsPage, error) {
	if req.Workload.IsZero() {
		return nil, ErrWorkloadRequired
	}
	r, err := s.resolveRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	before, err := decodeFlowsCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	rec, err := s.Store.GetWorkloadRecord(ctx, req.Workload)
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = flowstore.DefaultPageLimit
	}
	if limit > flowstore.MaxPageLimit {
		limit = flowstore.MaxPageLimit
	}
	q := flowstore.WindowPageQuery{
		WorkloadID: req.Workload, Since: r.From, Until: r.To,
		Decision: req.Verdict, Direction: req.Direction, PeerKey: req.Peer,
		Before: before, Limit: limit + 1, // one more tells whether a next page exists
	}
	if req.Service != nil {
		q.Protocol, q.DstPort = req.Service.Protocol, req.Service.Port
	}
	rows, err := s.Flows.ListWindowPage(ctx, q)
	if err != nil {
		return nil, err
	}
	names, err := s.loadNames(ctx)
	if err != nil {
		return nil, err
	}
	page := &FlowsPage{Workload: workloadRef(&rec.Workload), Range: r, Flows: make([]Flow, 0, len(rows))}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	for i := range rows {
		row := &rows[i]
		page.Flows = append(page.Flows, Flow{
			ID: row.ID, WindowStart: row.WindowStart, WindowEnd: row.WindowEnd,
			Peer: names.peer(row.Peer), SrcAddress: row.SrcAddress, DstAddress: row.DstAddress,
			Service: Service{Protocol: row.Protocol, Port: row.DstPort}, Direction: row.Direction, Verdict: row.Decision,
			Rule: ruleRef(row.MatchedRuleID), ConnectionCount: row.ConnectionCount, ByteCount: row.ByteCount,
			FirstSeen: row.FirstSeen, LastSeen: row.LastSeen, ProcessName: row.ProcessName,
		})
	}
	if more {
		last := rows[len(rows)-1]
		page.NextCursor = encodeFlowsCursor(flowstore.WindowCursor{WindowStart: last.WindowStart, ID: last.ID})
	}
	return page, nil
}
