package flowstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// GroupBy names one grouping of the windows the store serves. The set is
// closed: each member maps to one named statement in the store, chosen by
// the caller, and there is no free-form grouping (ADR-0007 as amended,
// ADR-0019). The names are the ones the operator surface accepts.
type GroupBy string

// The groupings the store serves. Inbound only in this version
// (ADR-0010), so the source of a flow is its resolved peer and the
// destination is the reporting workload; "peer" and "src" name that same
// resolved-peer dimension, each under the name the screen that uses it
// speaks.
const (
	// GroupByRule groups by the resolved rule that admitted the traffic;
	// records with no matched rule form one group with the empty rule id.
	// Rule hit counters.
	GroupByRule GroupBy = "rule"
	// GroupByRulePeer groups by rule and the resolved peer that hit it.
	GroupByRulePeer GroupBy = "rule,peer"
	// GroupBySrcDst groups by resolved peer and reporting workload: the
	// edges of the dependency map, and the rows of a would-block review.
	GroupBySrcDst GroupBy = "src,dst"
	// GroupByDstService groups by reporting workload and the service
	// (destination port and protocol) reached on it: the matrix cells.
	GroupByDstService GroupBy = "dst,service"
)

// GroupBys lists every grouping, in the order a document names them.
var GroupBys = []GroupBy{GroupByRule, GroupByRulePeer, GroupBySrcDst, GroupByDstService}

// ErrUnknownGroupBy is returned for a grouping outside GroupBys.
var ErrUnknownGroupBy = errors.New("flowstore: unknown grouping")

// ParseGroupBy resolves a comma-joined grouping, tolerating spaces around
// the names. It accepts the members of GroupBys and nothing else.
func ParseGroupBy(s string) (GroupBy, error) {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(parts[i]))
	}
	joined := GroupBy(strings.Join(parts, ","))
	for _, g := range GroupBys {
		if g == joined {
			return g, nil
		}
	}
	return "", fmt.Errorf("%w %q; one of %s", ErrUnknownGroupBy, s, GroupByNames())
}

// GroupByNames joins the allowed groupings for a message.
func GroupByNames() string {
	names := make([]string, 0, len(GroupBys))
	for _, g := range GroupBys {
		names = append(names, string(g))
	}
	return strings.Join(names, " | ")
}

// Keys returns the dimensions of the grouping, in order.
func (g GroupBy) Keys() []string {
	return strings.Split(string(g), ",")
}

// GroupOrder says how a grouped rollup is ordered before it is bounded.
type GroupOrder string

// Orders of a grouped rollup.
const (
	// OrderByConnections puts the busiest groups first. The default.
	OrderByConnections GroupOrder = "connections"
	// OrderByRecency puts the most recently seen groups first.
	OrderByRecency GroupOrder = "recent"
)

// DefaultGroupLimit is the number of groups a grouped rollup returns when
// the query sets none; MaxGroupLimit is the most it ever returns. A rollup
// with more groups than its limit returns the top groups in its order and
// says so; the screens show top-N with totals, never an unbounded list
// (ADR-0007 as amended).
const (
	DefaultGroupLimit = 200
	MaxGroupLimit     = 1000
)

// GroupQuery is a grouped rollup over the windows in [Since, Until). An
// empty WorkloadIDs means every workload; a zero Decision or Direction
// means any; a zero Protocol means every service, otherwise exactly the
// service (Protocol, DstPort).
type GroupQuery struct {
	GroupBy     GroupBy
	WorkloadIDs []identity.WorkloadID
	Since       time.Time
	Until       time.Time
	Decision    innerwallv1.PolicyDecision
	Direction   innerwallv1.Direction
	Protocol    innerwallv1.Protocol
	DstPort     uint16
	Order       GroupOrder
	// Limit bounds the groups returned; DefaultGroupLimit when zero,
	// never more than MaxGroupLimit.
	Limit int
}

// Group is one group of a grouped rollup. Which key fields are set
// depends on the grouping: RuleID for rule and rule,peer; Peer for
// rule,peer and src,dst; WorkloadID for src,dst and dst,service; DstPort
// and Protocol for dst,service. The peer's labels are the snapshot stored
// with its most recently seen record in the group (ADR-0019).
type Group struct {
	RuleID     string
	Peer       Peer
	WorkloadID identity.WorkloadID
	DstPort    uint16
	Protocol   innerwallv1.Protocol

	// FlowCount is the number of stored records in the group.
	FlowCount       int64
	ConnectionCount uint64
	ByteCount       uint64
	FirstSeen       time.Time
	LastSeen        time.Time
}

// GroupResult is a grouped rollup. EffectiveFrom and EffectiveTo are the
// bounds of the windows actually covered, which quantize the requested
// range to window boundaries; both are zero when no window matched.
// GroupCount and the totals cover every group, including those beyond
// the limit.
type GroupResult struct {
	Groups          []Group
	EffectiveFrom   time.Time
	EffectiveTo     time.Time
	GroupCount      int64
	FlowCount       int64
	ConnectionCount uint64
	ByteCount       uint64
}

// Truncated reports whether groups beyond those returned exist.
func (r *GroupResult) Truncated() bool {
	return r.GroupCount > int64(len(r.Groups))
}

// DefaultPageLimit is the number of windows a page holds when the query
// sets none; MaxPageLimit is the most a page ever holds.
const (
	DefaultPageLimit = 100
	MaxPageLimit     = 500
)

// WindowCursor is the position after which a page continues: the
// (window start, id) of the last row served, since pages run newest
// first.
type WindowCursor struct {
	WindowStart time.Time
	ID          int64
}

// WindowPageQuery selects one page of a workload's windows in [Since,
// Until), newest first, after Before. A nil Before is the first page. A
// zero Decision or Direction means any; an empty PeerKey means every
// peer; a zero Protocol means every service.
type WindowPageQuery struct {
	WorkloadID identity.WorkloadID
	Since      time.Time
	Until      time.Time
	Decision   innerwallv1.PolicyDecision
	Direction  innerwallv1.Direction
	PeerKey    string
	Protocol   innerwallv1.Protocol
	DstPort    uint16
	Before     *WindowCursor
	Limit      int
}

func boundedGroupLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultGroupLimit
	case n > MaxGroupLimit:
		return MaxGroupLimit
	default:
		return n
	}
}

func boundedPageLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultPageLimit
	case n > MaxPageLimit:
		return MaxPageLimit
	default:
		return n
	}
}
