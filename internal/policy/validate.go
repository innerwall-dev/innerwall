package policy

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Admission errors. Each is wrapped in a *ValidationError that names the
// offending field, so callers can test the kind with errors.Is and show the
// operator exactly what to fix.
var (
	ErrDirectionOutbound    = errors.New("policy: outbound rules are not admitted in this version; only DIRECTION_INBOUND is compiled (ADR-0010)")
	ErrDirectionUnspecified = errors.New("policy: rule direction must be stated; DIRECTION_UNSPECIFIED is not a direction")
	ErrEmptySelector        = errors.New("policy: selector is empty; an empty selector matches nothing, so state the breadth explicitly")
	ErrEmptyLabelKey        = errors.New("policy: label key must not be empty")
	ErrEmptyLabelValues     = errors.New("policy: label match lists no values; a match with no values can never be satisfied")
	ErrBadPortRange         = errors.New("policy: port range must satisfy start <= end <= 65535")
	ErrPortsOnPortless      = errors.New("policy: protocol has no ports; list none")
	ErrBadProtocol          = errors.New("policy: protocol must be tcp, udp, or icmp")
	ErrBadCIDR              = errors.New("policy: address is not a valid CIDR")
	ErrUnknownService       = errors.New("policy: rule references a service that does not exist")
	ErrUnknownAddressGroup  = errors.New("policy: rule references an address group that does not exist")
	ErrBadPeer              = errors.New("policy: peer must be exactly one of a workload selector, an address group, or a CIDR")
	ErrNoPeers              = errors.New("policy: rule lists no peers; a rule with no peers permits nothing")
	ErrNoServices           = errors.New("policy: rule lists no services; a rule with no services permits nothing")
	ErrNoEntries            = errors.New("policy: service defines no entries")
	ErrNoCIDRs              = errors.New("policy: address group lists no addresses")
	ErrEmptyName            = errors.New("policy: name must not be empty")
)

// ValidationError is an admission failure at a named location in the
// authored object.
type ValidationError struct {
	// Path locates the failure, e.g. "rules[1].peers[0].cidr".
	Path string
	// Err is one of the admission errors above.
	Err error
	// Detail is the offending value or a specific explanation, if any.
	Detail string
}

func (e *ValidationError) Error() string {
	msg := e.Path + ": " + e.Err.Error()
	if e.Detail != "" {
		msg += " (" + e.Detail + ")"
	}
	return msg
}

func (e *ValidationError) Unwrap() error { return e.Err }

func fail(path string, err error, detail string) error {
	return &ValidationError{Path: path, Err: err, Detail: detail}
}

// References is what admission needs to know about the rest of the
// authored model: which services and address groups exist.
type References struct {
	Services      map[uuid.UUID]struct{}
	AddressGroups map[uuid.UUID]struct{}
}

// ValidateService admits a service definition and normalizes nothing:
// what is persisted is what was validated.
func ValidateService(s *Service) error {
	if s.Name == "" {
		return fail("name", ErrEmptyName, "")
	}
	if len(s.Entries) == 0 {
		return fail("entries", ErrNoEntries, "")
	}
	for i := range s.Entries {
		if err := validateEntry(fmt.Sprintf("entries[%d]", i), &s.Entries[i]); err != nil {
			return err
		}
	}
	return nil
}

// ValidateAddressGroup admits an address group. CIDRs are checked to
// parse; the store persists them in canonical masked form.
func ValidateAddressGroup(g *AddressGroup) error {
	if g.Name == "" {
		return fail("name", ErrEmptyName, "")
	}
	if len(g.CIDRs) == 0 {
		return fail("cidrs", ErrNoCIDRs, "")
	}
	for i, c := range g.CIDRs {
		if _, err := netip.ParsePrefix(c); err != nil {
			return fail(fmt.Sprintf("cidrs[%d]", i), ErrBadCIDR, c)
		}
	}
	return nil
}

// ValidateRuleset admits a ruleset: direction (inbound only), selectors
// (never empty), peers (well-formed, references resolve), services
// (references resolve, ranges valid). It checks everything it can about a
// ruleset in isolation plus the references it is given; uniqueness of the
// name is the store's constraint.
func ValidateRuleset(rs *Ruleset, refs References) error {
	if rs.Name == "" {
		return fail("name", ErrEmptyName, "")
	}
	if err := validateSelector("scope", rs.Scope); err != nil {
		return err
	}
	for i := range rs.Rules {
		if err := validateRule(fmt.Sprintf("rules[%d]", i), &rs.Rules[i], refs); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(path string, r *Rule, refs References) error {
	switch r.Direction {
	case innerwallv1.Direction_DIRECTION_INBOUND:
	case innerwallv1.Direction_DIRECTION_OUTBOUND:
		return fail(path+".direction", ErrDirectionOutbound, "")
	case innerwallv1.Direction_DIRECTION_UNSPECIFIED:
		return fail(path+".direction", ErrDirectionUnspecified, "")
	default:
		return fail(path+".direction", ErrDirectionUnspecified, fmt.Sprintf("unknown direction %d", r.Direction))
	}
	if len(r.Peers) == 0 {
		return fail(path+".peers", ErrNoPeers, "")
	}
	for i := range r.Peers {
		if err := validatePeer(fmt.Sprintf("%s.peers[%d]", path, i), &r.Peers[i], refs); err != nil {
			return err
		}
	}
	if len(r.ServiceIDs) == 0 && len(r.Entries) == 0 {
		return fail(path+".services", ErrNoServices, "")
	}
	for i, id := range r.ServiceIDs {
		if _, ok := refs.Services[id]; !ok {
			return fail(fmt.Sprintf("%s.services[%d]", path, i), ErrUnknownService, id.String())
		}
	}
	for i := range r.Entries {
		if err := validateEntry(fmt.Sprintf("%s.entries[%d]", path, i), &r.Entries[i]); err != nil {
			return err
		}
	}
	return nil
}

func validatePeer(path string, p *Peer, refs References) error {
	set := 0
	if len(p.Workloads) > 0 || p.Kind == PeerWorkloads {
		set++
	}
	if p.AddressGroupID != uuid.Nil || p.Kind == PeerAddressGroup {
		set++
	}
	if p.CIDR != "" || p.Kind == PeerCIDR {
		set++
	}
	if set != 1 {
		return fail(path, ErrBadPeer, "")
	}
	switch p.Kind {
	case PeerWorkloads:
		return validateSelector(path+".workloads", p.Workloads)
	case PeerAddressGroup:
		if _, ok := refs.AddressGroups[p.AddressGroupID]; !ok {
			return fail(path+".address_group", ErrUnknownAddressGroup, p.AddressGroupID.String())
		}
		return nil
	case PeerCIDR:
		if _, err := netip.ParsePrefix(p.CIDR); err != nil {
			return fail(path+".cidr", ErrBadCIDR, p.CIDR)
		}
		return nil
	case PeerUnspecified:
		return fail(path, ErrBadPeer, "")
	default:
		return fail(path, ErrBadPeer, fmt.Sprintf("unknown peer kind %d", p.Kind))
	}
}

func validateSelector(path string, s Selector) error {
	if len(s) == 0 {
		return fail(path, ErrEmptySelector, "")
	}
	for key, values := range s {
		if key == "" {
			return fail(path, ErrEmptyLabelKey, "")
		}
		if len(values) == 0 {
			return fail(path+"["+key+"]", ErrEmptyLabelValues, "")
		}
	}
	return nil
}

func validateEntry(path string, e *ServiceEntry) error {
	switch e.Protocol {
	case innerwallv1.Protocol_PROTOCOL_TCP, innerwallv1.Protocol_PROTOCOL_UDP:
	case innerwallv1.Protocol_PROTOCOL_ICMP:
		if len(e.Ports) > 0 {
			return fail(path+".ports", ErrPortsOnPortless, "icmp")
		}
	case innerwallv1.Protocol_PROTOCOL_UNSPECIFIED:
		return fail(path+".protocol", ErrBadProtocol, "")
	default:
		return fail(path+".protocol", ErrBadProtocol, fmt.Sprintf("unknown protocol %d", e.Protocol))
	}
	for i, p := range e.Ports {
		if p.Start > p.End || p.End > 65535 {
			return fail(fmt.Sprintf("%s.ports[%d]", path, i), ErrBadPortRange, fmt.Sprintf("%d-%d", p.Start, p.End))
		}
	}
	return nil
}
