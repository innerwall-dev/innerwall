package policy

import (
	"time"

	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// PortRange is an inclusive port range; a single port has Start == End.
type PortRange struct {
	Start uint32
	End   uint32
}

// ServiceEntry is one protocol with the ports it permits. No ports means
// every port of the protocol, which is the only valid form for a protocol
// without ports.
type ServiceEntry struct {
	Protocol innerwallv1.Protocol
	Ports    []PortRange
}

// Service is a named, reusable set of entries.
type Service struct {
	ID        uuid.UUID
	Name      string
	Entries   []ServiceEntry
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AddressGroup is a named set of CIDRs for peers that are not managed
// workloads.
type AddressGroup struct {
	ID        uuid.UUID
	Name      string
	CIDRs     []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Selector matches workloads by label: every key must match (AND) and the
// label's value must be one of the listed values (OR). An empty selector
// matches nothing.
type Selector map[string][]string

// PeerKind says which field of a Peer is set.
type PeerKind int

// Peer kinds, in the order the wire contract's PeerSelector declares them.
const (
	PeerUnspecified PeerKind = iota
	PeerWorkloads
	PeerAddressGroup
	PeerCIDR
)

// Peer is the remote end of a rule: managed workloads by selector, a named
// address group, or a literal CIDR. Exactly one is set.
type Peer struct {
	Kind           PeerKind
	Workloads      Selector
	AddressGroupID uuid.UUID
	CIDR           string
}

// Rule is a single authored rule. The ruleset's scope is the local end,
// Peers the remote end, Direction orients the two. Services may be named
// by reference (ServiceIDs) or written inline (Entries); the renderer
// expands both into the same rendered form.
type Rule struct {
	ID          uuid.UUID
	Direction   innerwallv1.Direction
	Enabled     bool
	Description string
	Peers       []Peer
	ServiceIDs  []uuid.UUID
	Entries     []ServiceEntry
}

// Ruleset is a named set of rules applied to the workloads its scope
// selects.
type Ruleset struct {
	ID          uuid.UUID
	Name        string
	Description string
	Enabled     bool
	Scope       Selector
	Rules       []Rule
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Matches reports whether a workload carrying labels satisfies the
// selector. An empty selector matches nothing.
func (s Selector) Matches(labels map[string]string) bool {
	if len(s) == 0 {
		return false
	}
	for key, values := range s {
		have, ok := labels[key]
		if !ok {
			return false
		}
		matched := false
		for _, v := range values {
			if v == have {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
