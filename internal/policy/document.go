package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// The document form is how an operator writes a ruleset: JSON with names
// where the stored model has ids, string enums, and "80-90" port specs.
// It is the input of the command line today and of the API façade's
// import path later; the stored model never depends on it.

// RulesetDoc is the JSON form of a ruleset.
type RulesetDoc struct {
	ID          string              `json:"id,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Enabled     *bool               `json:"enabled,omitempty"`
	Scope       map[string][]string `json:"scope"`
	Rules       []RuleDoc           `json:"rules"`
}

// RuleDoc is the JSON form of a rule. Services and address groups may be
// named by name or id; Entries are inline protocol/port specs.
type RuleDoc struct {
	ID          string     `json:"id,omitempty"`
	Direction   string     `json:"direction"`
	Enabled     *bool      `json:"enabled,omitempty"`
	Description string     `json:"description,omitempty"`
	Peers       []PeerDoc  `json:"peers"`
	Services    []string   `json:"services,omitempty"`
	Entries     []EntryDoc `json:"entries,omitempty"`
}

// PeerDoc is the JSON form of a peer; exactly one field is set.
type PeerDoc struct {
	Workloads    map[string][]string `json:"workloads,omitempty"`
	AddressGroup string              `json:"address_group,omitempty"`
	CIDR         string              `json:"cidr,omitempty"`
}

// EntryDoc is the JSON form of a service entry: a protocol name and port
// specs such as "5432" or "6000-6010". No ports means every port.
type EntryDoc struct {
	Protocol string   `json:"protocol"`
	Ports    []string `json:"ports,omitempty"`
}

// Names resolves the names a document may use to ids and back.
type Names struct {
	ServiceByName      map[string]uuid.UUID
	ServiceName        map[uuid.UUID]string
	AddressGroupByName map[string]uuid.UUID
	AddressGroupName   map[uuid.UUID]string
}

// NewNames indexes services and address groups for document conversion.
func NewNames(services []Service, groups []AddressGroup) Names {
	n := Names{
		ServiceByName:      make(map[string]uuid.UUID, len(services)),
		ServiceName:        make(map[uuid.UUID]string, len(services)),
		AddressGroupByName: make(map[string]uuid.UUID, len(groups)),
		AddressGroupName:   make(map[uuid.UUID]string, len(groups)),
	}
	for _, s := range services {
		n.ServiceByName[s.Name] = s.ID
		n.ServiceName[s.ID] = s.Name
	}
	for _, g := range groups {
		n.AddressGroupByName[g.Name] = g.ID
		n.AddressGroupName[g.ID] = g.Name
	}
	return n
}

// ParseProtocol parses a protocol name.
func ParseProtocol(s string) (innerwallv1.Protocol, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tcp":
		return innerwallv1.Protocol_PROTOCOL_TCP, nil
	case "udp":
		return innerwallv1.Protocol_PROTOCOL_UDP, nil
	case "icmp":
		return innerwallv1.Protocol_PROTOCOL_ICMP, nil
	default:
		return innerwallv1.Protocol_PROTOCOL_UNSPECIFIED, fmt.Errorf("%w: %q", ErrBadProtocol, s)
	}
}

// ProtocolName is the inverse of ParseProtocol.
func ProtocolName(p innerwallv1.Protocol) string {
	return strings.ToLower(strings.TrimPrefix(p.String(), "PROTOCOL_"))
}

// ParseDirection parses a direction name.
func ParseDirection(s string) (innerwallv1.Direction, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "inbound":
		return innerwallv1.Direction_DIRECTION_INBOUND, nil
	case "outbound":
		return innerwallv1.Direction_DIRECTION_OUTBOUND, nil
	case "":
		return innerwallv1.Direction_DIRECTION_UNSPECIFIED, nil
	default:
		return innerwallv1.Direction_DIRECTION_UNSPECIFIED, fmt.Errorf("policy: unknown direction %q", s)
	}
}

// DirectionName is the inverse of ParseDirection.
func DirectionName(d innerwallv1.Direction) string {
	return strings.ToLower(strings.TrimPrefix(d.String(), "DIRECTION_"))
}

// ParseMode parses an enforcement mode name.
func ParseMode(s string) (innerwallv1.EnforcementMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "visibility":
		return innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, nil
	case "simulation":
		return innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, nil
	case "enforced":
		return innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, nil
	default:
		return innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED, fmt.Errorf("policy: mode must be visibility, simulation, or enforced; got %q", s)
	}
}

// ModeName is the inverse of ParseMode.
func ModeName(m innerwallv1.EnforcementMode) string {
	return strings.ToLower(strings.TrimPrefix(m.String(), "ENFORCEMENT_MODE_"))
}

// ParsePortSpec parses "80" or "8000-8010" into an inclusive range. Bounds
// are checked by admission, not here, so a bad range reports precisely.
func ParsePortSpec(s string) (PortRange, error) {
	s = strings.TrimSpace(s)
	lo, hi, isRange := strings.Cut(s, "-")
	start, err := strconv.ParseUint(strings.TrimSpace(lo), 10, 32)
	if err != nil {
		return PortRange{}, fmt.Errorf("policy: port %q is not a number", lo)
	}
	end := start
	if isRange {
		end, err = strconv.ParseUint(strings.TrimSpace(hi), 10, 32)
		if err != nil {
			return PortRange{}, fmt.Errorf("policy: port %q is not a number", hi)
		}
	}
	return PortRange{Start: uint32(start), End: uint32(end)}, nil
}

// FormatPortSpec is the inverse of ParsePortSpec.
func FormatPortSpec(p PortRange) string {
	if p.Start == p.End {
		return strconv.FormatUint(uint64(p.Start), 10)
	}
	return fmt.Sprintf("%d-%d", p.Start, p.End)
}

// ParseEntrySpec parses the command-line entry form "tcp:5432,6000-6010"
// or "icmp" (no ports).
func ParseEntrySpec(s string) (ServiceEntry, error) {
	protoName, ports, _ := strings.Cut(s, ":")
	proto, err := ParseProtocol(protoName)
	if err != nil {
		return ServiceEntry{}, err
	}
	e := ServiceEntry{Protocol: proto}
	if strings.TrimSpace(ports) == "" {
		return e, nil
	}
	for _, spec := range strings.Split(ports, ",") {
		p, err := ParsePortSpec(spec)
		if err != nil {
			return ServiceEntry{}, err
		}
		e.Ports = append(e.Ports, p)
	}
	return e, nil
}

// FormatEntrySpec is the inverse of ParseEntrySpec.
func FormatEntrySpec(e ServiceEntry) string {
	if len(e.Ports) == 0 {
		return ProtocolName(e.Protocol)
	}
	specs := make([]string, 0, len(e.Ports))
	for _, p := range e.Ports {
		specs = append(specs, FormatPortSpec(p))
	}
	return ProtocolName(e.Protocol) + ":" + strings.Join(specs, ",")
}

func entryFromDoc(d EntryDoc) (ServiceEntry, error) {
	proto, err := ParseProtocol(d.Protocol)
	if err != nil {
		return ServiceEntry{}, err
	}
	e := ServiceEntry{Protocol: proto}
	for _, spec := range d.Ports {
		p, err := ParsePortSpec(spec)
		if err != nil {
			return ServiceEntry{}, err
		}
		e.Ports = append(e.Ports, p)
	}
	return e, nil
}

func entryToDoc(e ServiceEntry) EntryDoc {
	d := EntryDoc{Protocol: ProtocolName(e.Protocol)}
	for _, p := range e.Ports {
		d.Ports = append(d.Ports, FormatPortSpec(p))
	}
	return d
}

// ResolveID accepts an id or a name and returns the id.
func ResolveID(s string, byName map[string]uuid.UUID) (uuid.UUID, bool) {
	if id, err := uuid.Parse(s); err == nil {
		return id, true
	}
	id, ok := byName[s]
	return id, ok
}

// DecodeRuleset parses a JSON document into a ruleset, resolving names.
// It does not validate; admission does, with the store's view of what
// exists. An unresolvable name is reported here because it is a document
// error, not an admission error.
func DecodeRuleset(data []byte, names Names) (*Ruleset, error) {
	var doc RulesetDoc
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("policy: parsing ruleset document: %w", err)
	}
	return RulesetFromDoc(&doc, names)
}

// RulesetFromDoc converts a parsed document.
func RulesetFromDoc(doc *RulesetDoc, names Names) (*Ruleset, error) {
	rs := &Ruleset{Name: doc.Name, Description: doc.Description, Enabled: true, Scope: Selector(doc.Scope)}
	if doc.Enabled != nil {
		rs.Enabled = *doc.Enabled
	}
	if doc.ID != "" {
		id, err := uuid.Parse(doc.ID)
		if err != nil {
			return nil, fmt.Errorf("policy: ruleset id: %w", err)
		}
		rs.ID = id
	}
	if rs.Scope == nil {
		rs.Scope = Selector{}
	}
	for i, rd := range doc.Rules {
		r := Rule{Description: rd.Description, Enabled: true}
		if rd.Enabled != nil {
			r.Enabled = *rd.Enabled
		}
		if rd.ID != "" {
			id, err := uuid.Parse(rd.ID)
			if err != nil {
				return nil, fmt.Errorf("policy: rules[%d].id: %w", i, err)
			}
			r.ID = id
		}
		dir, err := ParseDirection(rd.Direction)
		if err != nil {
			return nil, fmt.Errorf("policy: rules[%d].direction: %w", i, err)
		}
		r.Direction = dir
		for j, pd := range rd.Peers {
			p := Peer{}
			switch {
			case pd.Workloads != nil:
				p.Kind, p.Workloads = PeerWorkloads, Selector(pd.Workloads)
			case pd.AddressGroup != "":
				id, ok := ResolveID(pd.AddressGroup, names.AddressGroupByName)
				if !ok {
					return nil, fmt.Errorf("policy: rules[%d].peers[%d]: %w: %q", i, j, ErrUnknownAddressGroup, pd.AddressGroup)
				}
				p.Kind, p.AddressGroupID = PeerAddressGroup, id
			case pd.CIDR != "":
				p.Kind, p.CIDR = PeerCIDR, pd.CIDR
			}
			r.Peers = append(r.Peers, p)
		}
		for j, name := range rd.Services {
			id, ok := ResolveID(name, names.ServiceByName)
			if !ok {
				return nil, fmt.Errorf("policy: rules[%d].services[%d]: %w: %q", i, j, ErrUnknownService, name)
			}
			r.ServiceIDs = append(r.ServiceIDs, id)
		}
		for j, ed := range rd.Entries {
			e, err := entryFromDoc(ed)
			if err != nil {
				return nil, fmt.Errorf("policy: rules[%d].entries[%d]: %w", i, j, err)
			}
			r.Entries = append(r.Entries, e)
		}
		rs.Rules = append(rs.Rules, r)
	}
	return rs, nil
}

// RulesetToDoc converts a ruleset to its document form, using names where
// they resolve and ids otherwise.
func RulesetToDoc(rs *Ruleset, names Names) *RulesetDoc {
	enabled := rs.Enabled
	doc := &RulesetDoc{ID: rs.ID.String(), Name: rs.Name, Description: rs.Description, Enabled: &enabled, Scope: rs.Scope, Rules: []RuleDoc{}}
	for i := range rs.Rules {
		r := &rs.Rules[i]
		en := r.Enabled
		rd := RuleDoc{ID: r.ID.String(), Direction: DirectionName(r.Direction), Enabled: &en, Description: r.Description, Peers: []PeerDoc{}}
		for _, p := range r.Peers {
			switch p.Kind {
			case PeerWorkloads:
				rd.Peers = append(rd.Peers, PeerDoc{Workloads: p.Workloads})
			case PeerAddressGroup:
				name, ok := names.AddressGroupName[p.AddressGroupID]
				if !ok {
					name = p.AddressGroupID.String()
				}
				rd.Peers = append(rd.Peers, PeerDoc{AddressGroup: name})
			case PeerCIDR:
				rd.Peers = append(rd.Peers, PeerDoc{CIDR: p.CIDR})
			case PeerUnspecified:
			}
		}
		for _, id := range r.ServiceIDs {
			name, ok := names.ServiceName[id]
			if !ok {
				name = id.String()
			}
			rd.Services = append(rd.Services, name)
		}
		for _, e := range r.Entries {
			rd.Entries = append(rd.Entries, entryToDoc(e))
		}
		doc.Rules = append(doc.Rules, rd)
	}
	return doc
}

// ErrNoDocument is returned when a command expects a document and none is
// given.
var ErrNoDocument = errors.New("policy: a ruleset document is required")
