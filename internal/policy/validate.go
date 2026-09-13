package policy

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Admission errors. Each is wrapped in a *ValidationError that names the
// offending field, so callers can test the kind with errors.Is and show the
// operator exactly what to fix.
var (
	ErrDirectionOutbound    = errors.New("policy: outbound rules are not admitted in this version; only DIRECTION_INBOUND is compiled (ADR-0010)")
	ErrDirectionUnspecified = errors.New("policy: rule direction must be stated; DIRECTION_UNSPECIFIED is not a direction")
	ErrBadDirection         = errors.New("policy: direction must be inbound or outbound")
	ErrEmptySelector        = errors.New("policy: selector is empty; an empty selector matches nothing, so state the breadth explicitly")
	ErrEmptyLabelKey        = errors.New("policy: label key must not be empty")
	ErrEmptyLabelValues     = errors.New("policy: label match lists no values; a match with no values can never be satisfied")
	ErrBadPortRange         = errors.New("policy: port range must satisfy start <= end <= 65535")
	ErrBadPortSpec          = errors.New("policy: port must be a number or a start-end range")
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
	ErrBadID                = errors.New("policy: id is not a UUID")
	ErrDuplicateRule        = errors.New("policy: a rule id appears more than once in the ruleset")
)

// ruleNames are the stable names of the admission rules, one per error,
// which a client branches on for field-level display. They are part of the
// surface's contract and do not change when a message is reworded.
var ruleNames = map[error]string{
	ErrDirectionOutbound:    "direction-outbound",
	ErrDirectionUnspecified: "direction-required",
	ErrBadDirection:         "direction",
	ErrEmptySelector:        "selector-empty",
	ErrEmptyLabelKey:        "label-key-required",
	ErrEmptyLabelValues:     "label-values-required",
	ErrBadPortRange:         "port-range",
	ErrBadPortSpec:          "port-spec",
	ErrPortsOnPortless:      "ports-on-portless-protocol",
	ErrBadProtocol:          "protocol",
	ErrBadCIDR:              "cidr",
	ErrUnknownService:       "service-unknown",
	ErrUnknownAddressGroup:  "address-group-unknown",
	ErrBadPeer:              "peer-shape",
	ErrNoPeers:              "peers-required",
	ErrNoServices:           "services-required",
	ErrNoEntries:            "entries-required",
	ErrNoCIDRs:              "cidrs-required",
	ErrEmptyName:            "name-required",
	ErrBadID:                "id",
	ErrDuplicateRule:        "rule-id-duplicate",
}

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

// Rule is the stable name of the admission rule the finding violates.
func (e *ValidationError) Rule() string {
	if name, ok := ruleNames[e.Err]; ok {
		return name
	}
	return "invalid"
}

// Message is the finding for a person: the rule's explanation with the
// offending value, without the package prefix a log line carries.
func (e *ValidationError) Message() string {
	msg := strings.TrimPrefix(e.Err.Error(), "policy: ")
	if e.Detail != "" {
		msg += " (" + e.Detail + ")"
	}
	return msg
}

// Findings is every admission failure of one object, in document order.
// Admission reports all of them at once so an editor can mark every field
// that needs attention; errors.Is and errors.As see through to each one.
type Findings struct {
	Errors []*ValidationError
}

func (f *Findings) Error() string {
	parts := make([]string, 0, len(f.Errors))
	for _, e := range f.Errors {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}

// Unwrap exposes every finding to errors.Is and errors.As.
func (f *Findings) Unwrap() []error {
	out := make([]error, 0, len(f.Errors))
	for _, e := range f.Errors {
		out = append(out, e)
	}
	return out
}

func (f *Findings) add(path string, err error, detail string) {
	f.Errors = append(f.Errors, &ValidationError{Path: path, Err: err, Detail: detail})
}

// result is nil when nothing was found, so a validator returns a plain
// nil error rather than a typed nil.
func (f *Findings) result() error {
	if len(f.Errors) == 0 {
		return nil
	}
	return f
}

// Rebase strips prefix from every finding's path, for a caller that
// validated a whole object on behalf of one part of it.
func (f *Findings) Rebase(prefix string) {
	for _, e := range f.Errors {
		e.Path = strings.TrimPrefix(strings.TrimPrefix(e.Path, prefix), ".")
	}
}

// AsFindings returns the findings an error carries, or nil when it is not
// an admission failure.
func AsFindings(err error) *Findings {
	var f *Findings
	if errors.As(err, &f) {
		return f
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return &Findings{Errors: []*ValidationError{ve}}
	}
	return nil
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
	f := &Findings{}
	if s.Name == "" {
		f.add("name", ErrEmptyName, "")
	}
	if len(s.Entries) == 0 {
		f.add("entries", ErrNoEntries, "")
	}
	for i := range s.Entries {
		validateEntry(f, fmt.Sprintf("entries[%d]", i), &s.Entries[i])
	}
	return f.result()
}

// ValidateAddressGroup admits an address group. CIDRs are checked to
// parse; the store persists them in canonical masked form.
func ValidateAddressGroup(g *AddressGroup) error {
	f := &Findings{}
	if g.Name == "" {
		f.add("name", ErrEmptyName, "")
	}
	if len(g.CIDRs) == 0 {
		f.add("cidrs", ErrNoCIDRs, "")
	}
	for i, c := range g.CIDRs {
		if _, err := netip.ParsePrefix(c); err != nil {
			f.add(fmt.Sprintf("cidrs[%d]", i), ErrBadCIDR, c)
		}
	}
	return f.result()
}

// ValidateRuleset admits a ruleset: direction (inbound only), selectors
// (never empty), peers (well-formed, references resolve), services
// (references resolve, ranges valid), rule ids (distinct). It checks
// everything it can about a ruleset in isolation plus the references it
// is given; uniqueness of the name is the store's constraint.
func ValidateRuleset(rs *Ruleset, refs References) error {
	f := &Findings{}
	if rs.Name == "" {
		f.add("name", ErrEmptyName, "")
	}
	validateSelector(f, "scope", rs.Scope)
	seen := map[uuid.UUID]int{}
	for i := range rs.Rules {
		if id := rs.Rules[i].ID; id != uuid.Nil {
			if first, dup := seen[id]; dup {
				f.add(fmt.Sprintf("rules[%d].id", i), ErrDuplicateRule, fmt.Sprintf("also rules[%d]", first))
			} else {
				seen[id] = i
			}
		}
		validateRule(f, fmt.Sprintf("rules[%d]", i), &rs.Rules[i], refs)
	}
	return f.result()
}

// ValidateRule admits one rule on its own, reporting paths relative to
// the rule.
func ValidateRule(r *Rule, refs References) error {
	f := &Findings{}
	validateRule(f, "", r, refs)
	f.Rebase("")
	return f.result()
}

// ValidateSelector admits a selector on its own: never empty, every key
// named, every key with at least one value.
func ValidateSelector(s Selector) error {
	f := &Findings{}
	validateSelector(f, "", s)
	f.Rebase("")
	return f.result()
}

func join(path, field string) string {
	if path == "" {
		return field
	}
	return path + "." + field
}

func validateRule(f *Findings, path string, r *Rule, refs References) {
	switch r.Direction {
	case innerwallv1.Direction_DIRECTION_INBOUND:
	case innerwallv1.Direction_DIRECTION_OUTBOUND:
		f.add(join(path, "direction"), ErrDirectionOutbound, "")
	case innerwallv1.Direction_DIRECTION_UNSPECIFIED:
		f.add(join(path, "direction"), ErrDirectionUnspecified, "")
	default:
		f.add(join(path, "direction"), ErrDirectionUnspecified, fmt.Sprintf("unknown direction %d", r.Direction))
	}
	if len(r.Peers) == 0 {
		f.add(join(path, "peers"), ErrNoPeers, "")
	}
	for i := range r.Peers {
		validatePeer(f, fmt.Sprintf("%s[%d]", join(path, "peers"), i), &r.Peers[i], refs)
	}
	if len(r.ServiceIDs) == 0 && len(r.Entries) == 0 {
		f.add(join(path, "services"), ErrNoServices, "")
	}
	for i, id := range r.ServiceIDs {
		if _, ok := refs.Services[id]; !ok {
			f.add(fmt.Sprintf("%s[%d]", join(path, "services"), i), ErrUnknownService, id.String())
		}
	}
	for i := range r.Entries {
		validateEntry(f, fmt.Sprintf("%s[%d]", join(path, "entries"), i), &r.Entries[i])
	}
}

func validatePeer(f *Findings, path string, p *Peer, refs References) {
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
		f.add(path, ErrBadPeer, "")
		return
	}
	switch p.Kind {
	case PeerWorkloads:
		validateSelector(f, path+".workloads", p.Workloads)
	case PeerAddressGroup:
		if _, ok := refs.AddressGroups[p.AddressGroupID]; !ok {
			f.add(path+".address_group", ErrUnknownAddressGroup, p.AddressGroupID.String())
		}
	case PeerCIDR:
		if _, err := netip.ParsePrefix(p.CIDR); err != nil {
			f.add(path+".cidr", ErrBadCIDR, p.CIDR)
		}
	case PeerUnspecified:
		f.add(path, ErrBadPeer, "")
	default:
		f.add(path, ErrBadPeer, fmt.Sprintf("unknown peer kind %d", p.Kind))
	}
}

func validateSelector(f *Findings, path string, s Selector) {
	if len(s) == 0 {
		f.add(path, ErrEmptySelector, "")
		return
	}
	for key, values := range s {
		if key == "" {
			f.add(path, ErrEmptyLabelKey, "")
		}
		if len(values) == 0 {
			f.add(path+"["+key+"]", ErrEmptyLabelValues, "")
		}
	}
}

func validateEntry(f *Findings, path string, e *ServiceEntry) {
	switch e.Protocol {
	case innerwallv1.Protocol_PROTOCOL_TCP, innerwallv1.Protocol_PROTOCOL_UDP:
	case innerwallv1.Protocol_PROTOCOL_ICMP:
		if len(e.Ports) > 0 {
			f.add(path+".ports", ErrPortsOnPortless, "icmp")
		}
	case innerwallv1.Protocol_PROTOCOL_UNSPECIFIED:
		f.add(path+".protocol", ErrBadProtocol, "")
	default:
		f.add(path+".protocol", ErrBadProtocol, fmt.Sprintf("unknown protocol %d", e.Protocol))
	}
	for i, p := range e.Ports {
		if p.Start > p.End || p.End > 65535 {
			f.add(fmt.Sprintf("%s.ports[%d]", path, i), ErrBadPortRange, fmt.Sprintf("%d-%d", p.Start, p.End))
		}
	}
}
