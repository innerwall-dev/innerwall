package readmodel

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// Errors a caller branches on. A transport maps each to its own problem;
// the command line prints them.
var (
	// ErrInvalidRange is returned when a range's end is not after its
	// start.
	ErrInvalidRange = errors.New("readmodel: the end of the range must be after its start")
	// ErrInvalidCursor is returned for a cursor this read model did not
	// issue.
	ErrInvalidCursor = errors.New("readmodel: the cursor is not one this read issued")
	// ErrWorkloadRequired is returned by a read that has no unbounded
	// form when it is asked for one.
	ErrWorkloadRequired = errors.New("readmodel: a workload is required")
	// ErrInvalidService is returned for a service that is not
	// <protocol>/<port> or icmp.
	ErrInvalidService = errors.New("readmodel: a service is <protocol>/<port>, or icmp")
)

// DefaultRange is the range a read covers when none is given: the last
// day up to now.
const DefaultRange = 24 * time.Hour

// Store is the persisted state the read model reads. The Postgres store
// implements it with hand-written SQL (ADR-0006); flow data is read
// through the FlowStore beside it (ADR-0009).
type Store interface {
	// ListWorkloads returns every workload with its labels and addresses,
	// the registry's view for naming peers and resolving scopes.
	ListWorkloads(ctx context.Context) ([]registry.Workload, error)
	ListAddressGroups(ctx context.Context) ([]policy.AddressGroup, error)
	ListRulesets(ctx context.Context) ([]policy.Ruleset, error)
	// GetWorkloadPolicy returns the persisted rendered policy of a
	// workload with its version, or nil when none has been rendered.
	GetWorkloadPolicy(ctx context.Context, id identity.WorkloadID) (*innerwallv1.WorkloadPolicy, error)
	// ListWorkloadPage returns one page of the fleet in fleet order.
	ListWorkloadPage(ctx context.Context, q WorkloadPageQuery) ([]WorkloadRecord, error)
	// GetWorkloadRecord returns one workload with its latest rendered
	// version and listening services, or registry.ErrWorkloadUnknown.
	GetWorkloadRecord(ctx context.Context, id identity.WorkloadID) (*WorkloadRecord, error)
	// ListWorkloadLabelIndex returns every workload's current labels, for
	// resolving a selector to the workloads in its scope.
	ListWorkloadLabelIndex(ctx context.Context) (map[identity.WorkloadID]map[string]string, error)
}

// Reader is the read model. The command line and the operator surface
// hold one each over the same store.
type Reader struct {
	Store Store
	Flows flowstore.FlowStore
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

func (s *Reader) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Range is a half-open time range [From, To).
type Range struct {
	From time.Time
	To   time.Time
}

// resolveRange fills a partial range: a zero To is now, a zero From is
// DefaultRange before To.
func (s *Reader) resolveRange(from, to time.Time) (Range, error) {
	if to.IsZero() {
		to = s.now()
	}
	if from.IsZero() {
		from = to.Add(-DefaultRange)
	}
	if !to.After(from) {
		return Range{}, ErrInvalidRange
	}
	return Range{From: from.UTC(), To: to.UTC()}, nil
}

// Service is a destination port and protocol, the unit a flow record and
// a rendered rule share. A protocol without ports (ICMP) has Port 0.
type Service struct {
	Protocol innerwallv1.Protocol
	Port     uint16
}

// ParseService parses "<protocol>/<port>" or "icmp".
func ParseService(s string) (Service, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	protoName, portText, hasPort := strings.Cut(s, "/")
	proto, err := policy.ParseProtocol(protoName)
	if err != nil {
		return Service{}, fmt.Errorf("%w: %q", ErrInvalidService, s)
	}
	if proto == innerwallv1.Protocol_PROTOCOL_ICMP {
		if hasPort && portText != "" && portText != "0" {
			return Service{}, fmt.Errorf("%w: %q", ErrInvalidService, s)
		}
		return Service{Protocol: proto}, nil
	}
	if !hasPort {
		return Service{}, fmt.Errorf("%w: %q", ErrInvalidService, s)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return Service{}, fmt.Errorf("%w: %q", ErrInvalidService, s)
	}
	return Service{Protocol: proto, Port: uint16(port)}, nil
}

// String is the inverse of ParseService.
func (s Service) String() string {
	if s.Protocol == innerwallv1.Protocol_PROTOCOL_ICMP {
		return policy.ProtocolName(s.Protocol)
	}
	return fmt.Sprintf("%s/%d", policy.ProtocolName(s.Protocol), s.Port)
}

// RuleRef names a resolved rule as flow records attribute traffic to it:
// the resolved id, and the authored rule and protocol it was rendered
// from (ADR-0018).
type RuleRef struct {
	ID             string
	AuthoredRuleID string
	Protocol       innerwallv1.Protocol
}

func ruleRef(id string) *RuleRef {
	if id == "" {
		return nil
	}
	ref := &RuleRef{ID: id}
	// A stored id that does not carry provenance is shown as it is.
	if authored, proto, err := provenance(id); err == nil {
		ref.AuthoredRuleID, ref.Protocol = authored, proto
	}
	return ref
}

// PeerRef is a resolved peer as ingestion stored it (ADR-0019): its kind
// and key, the label snapshot captured at the time for a workload peer,
// and the name the registry currently gives the identity (a workload's
// hostname, an address group's name), which is looked up by the stored
// identity for display and is empty when the identity is no longer
// registered. An unresolved peer's key is its address.
type PeerRef struct {
	Kind   flowstore.PeerKind
	Key    string
	Name   string
	Labels map[string]string
}

// WorkloadRef names a workload as the reporting end of a flow, with the
// registry's current hostname and labels.
type WorkloadRef struct {
	ID       identity.WorkloadID
	Hostname string
	Labels   map[string]string
}

// names resolves stored identities to current display names. It is built
// once per read from the registry as it stands.
type names struct {
	workloads map[string]*registry.Workload
	groups    map[string]string
}

func (s *Reader) loadNames(ctx context.Context) (*names, error) {
	workloads, err := s.Store.ListWorkloads(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := s.Store.ListAddressGroups(ctx)
	if err != nil {
		return nil, err
	}
	n := &names{workloads: make(map[string]*registry.Workload, len(workloads)), groups: make(map[string]string, len(groups))}
	for i := range workloads {
		n.workloads[workloads[i].ID.String()] = &workloads[i]
	}
	for i := range groups {
		n.groups[groups[i].ID.String()] = groups[i].Name
	}
	return n, nil
}

func (n *names) peer(p flowstore.Peer) PeerRef {
	ref := PeerRef{Kind: p.Kind, Key: p.Key, Labels: p.Labels}
	if ref.Labels == nil {
		ref.Labels = map[string]string{}
	}
	switch p.Kind {
	case flowstore.PeerWorkload:
		if w, ok := n.workloads[p.Key]; ok {
			ref.Name = w.Hostname
		}
	case flowstore.PeerAddressGroup:
		ref.Name = n.groups[p.Key]
	case flowstore.PeerUnknown:
	}
	return ref
}

func (n *names) workload(id identity.WorkloadID) WorkloadRef {
	ref := WorkloadRef{ID: id, Labels: map[string]string{}}
	if w, ok := n.workloads[id.String()]; ok {
		ref.Hostname = w.Hostname
		ref.Labels = w.LabelMap()
	}
	return ref
}

func workloadRef(w *registry.Workload) WorkloadRef {
	return WorkloadRef{ID: w.ID, Hostname: w.Hostname, Labels: w.LabelMap()}
}

// scope resolves the workloads a read covers: one workload, the workloads
// a selector currently matches, or both intersected. It reports whether a
// scope was given at all (an empty result with none given means every
// workload) and, when one was, whether it is empty, so the caller never
// turns "no workload matched" into "every workload".
func (s *Reader) scope(ctx context.Context, workload *identity.WorkloadID, selector policy.Selector) (ids []identity.WorkloadID, scoped bool, err error) {
	if workload != nil {
		if _, err := s.Store.GetWorkloadRecord(ctx, *workload); err != nil {
			return nil, true, err
		}
		ids = []identity.WorkloadID{*workload}
		scoped = true
	}
	if len(selector) == 0 {
		return ids, scoped, nil
	}
	index, err := s.Store.ListWorkloadLabelIndex(ctx)
	if err != nil {
		return nil, true, err
	}
	var matched []identity.WorkloadID
	for id, labels := range index {
		if !selector.Matches(labels) {
			continue
		}
		if workload != nil && id != *workload {
			continue
		}
		matched = append(matched, id)
	}
	sortIDs(matched)
	return matched, true, nil
}

func sortIDs(ids []identity.WorkloadID) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j].String() < ids[j-1].String(); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

// --- cursors -----------------------------------------------------------------

// A cursor is opaque to callers: a versioned, base64-encoded position
// that only the read that issued it interprets. The version letter says
// which read it belongs to, so a flows cursor handed to the workloads
// read is refused rather than misread.

func encodeCursor(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "|")))
}

func decodeCursor(token string, kind string, n int) ([]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != n+1 || parts[0] != kind {
		return nil, ErrInvalidCursor
	}
	return parts[1:], nil
}

func cursorTime(s string) (time.Time, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, ErrInvalidCursor
	}
	return time.Unix(0, n).UTC(), nil
}

func cursorUUID(s string) (uuid.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, ErrInvalidCursor
	}
	return u, nil
}
