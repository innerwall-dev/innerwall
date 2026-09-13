// Package nft is the Linux enforcement backend: it installs a rendered
// WorkloadPolicy into one nftables table this agent owns, as a single
// atomic transaction, and never touches state outside that table
// (ADR-0003, ADR-0020).
//
// The table holds one named set per resolved rule and address family,
// carrying the rule's peer prefixes, and one base chain on the input hook.
// The chain accepts loopback, accepts established and related traffic,
// then accepts what each rule permits (source in the rule's set, the
// rule's protocol and ports) while marking the connection with the rule's
// index, and ends in a terminal rule that decides what the policy did not
// permit: in enforced mode it logs the packet and drops it; in simulation
// mode it logs the packet, marks the connection as one enforcement would
// have dropped, and accepts it. Everything else is identical between the
// two modes, so a simulation verdict is evidence about exactly the
// ruleset enforcement would install. Visibility mode installs the table
// with no chain at all.
package nft

import (
	"fmt"
	"sort"
	"strings"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// DefaultTable is the name of the owned table.
const DefaultTable = "innerwall"

// DefaultNflogGroup is the netlink log group the terminal rule logs to.
const DefaultNflogGroup uint16 = 200

// ChainName is the one base chain in the owned table.
const ChainName = "inbound"

// Priority is the input-hook priority of the chain, as nft spells it: ten
// after the conventional filter priority, so that a host's own filter
// chains run first and their verdict on traffic they reject stands. An
// accept in one base chain never skips another, so both this chain and
// the host's must accept a packet for it to be delivered; a drop in
// either is final. That is the coexistence stance: the agent adds
// constraints and removes none (ADR-0003).
const Priority = "filter + 10"

// WouldBlockMark is the connection mark the simulation terminal rule sets
// before accepting: the mark of a connection enforcement would have
// dropped. Rule marks are small positive integers and never reach it.
const WouldBlockMark uint32 = 0xffffffff

// LogPrefix is the prefix on every logged packet.
const LogPrefix = "innerwall "

// Options parameterize a rendering.
type Options struct {
	// Table is the owned table's name; DefaultTable when empty.
	Table string
	// NflogGroup is the log group; DefaultNflogGroup when zero.
	NflogGroup uint16
}

func (o Options) table() string {
	if o.Table != "" {
		return o.Table
	}
	return DefaultTable
}

func (o Options) group() uint16 {
	if o.NflogGroup != 0 {
		return o.NflogGroup
	}
	return DefaultNflogGroup
}

// Marks returns the connection mark of every rule in the canonical policy:
// rules in canonical order (sorted by id) are numbered from 1. The mapping
// is a pure function of the policy, so the collector's classification of a
// marked connection needs nothing beyond the policy that set the mark.
func Marks(policy *innerwallv1.WorkloadPolicy) map[uint32]string {
	out := map[uint32]string{}
	for i, r := range rendered.Canonical(policy).GetInboundRules() {
		out[uint32(i+1)] = r.GetRuleId() //nolint:gosec // rule counts are small
	}
	return out
}

// SetName returns the name of the set holding a rule's peers for one
// address family. The rule id is kept whole, with every character that is
// not a letter, digit, or underscore replaced by an underscore, so that
// the authored rule id can be read back from `nft list ruleset` alone.
func SetName(ruleID string, v6 bool) string {
	var b strings.Builder
	b.WriteString("r_")
	for _, c := range ruleID {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
	}
	if v6 {
		b.WriteString("_v6")
	} else {
		b.WriteString("_v4")
	}
	return b.String()
}

// Render returns the nft script that installs policy as the complete
// content of the owned table, as one transaction: the table is declared
// (a no-op when it exists), deleted, and recreated with its new content,
// so the kernel holds either the previous ruleset or this one and never a
// mixture (ADR-0003, ADR-0015).
func Render(policy *innerwallv1.WorkloadPolicy, opts Options) string {
	policy = rendered.Canonical(policy)
	table := opts.table()
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s {}\n", table)
	fmt.Fprintf(&b, "delete table inet %s\n", table)
	fmt.Fprintf(&b, "table inet %s {\n", table)
	if policy.GetMode() == innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY || policy.GetMode() == innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED {
		// Visibility installs no verdict chain: nothing is evaluated,
		// nothing is dropped, and collection continues from conntrack.
		b.WriteString("}\n")
		return b.String()
	}
	rules := policy.GetInboundRules()
	for _, r := range rules {
		v4, v6 := splitPeers(r.GetPeerCidrs())
		writeSet(&b, SetName(r.GetRuleId(), false), "ipv4_addr", v4)
		writeSet(&b, SetName(r.GetRuleId(), true), "ipv6_addr", v6)
	}
	fmt.Fprintf(&b, "\tchain %s {\n", ChainName)
	fmt.Fprintf(&b, "\t\ttype filter hook input priority %s; policy accept;\n", Priority)
	b.WriteString("\t\tiif \"lo\" accept\n")
	// Established and related traffic is accepted before any rule. With
	// inbound-only scope (ADR-0010) this is what makes it impossible for
	// the agent to sever its own control-plane stream: that stream is an
	// outbound connection the agent initiated, its return traffic is
	// established, and nothing after this line can touch it.
	b.WriteString("\t\tct state established,related accept\n")
	for i, r := range rules {
		mark := uint32(i + 1) //nolint:gosec // rule counts are small
		comment := fmt.Sprintf("innerwall rule %s mark %d", r.GetRuleId(), mark)
		match := l4Match(r)
		fmt.Fprintf(&b, "\t\tip saddr @%s %s ct mark set %d accept comment \"%s\"\n", SetName(r.GetRuleId(), false), match.v4, mark, comment)
		fmt.Fprintf(&b, "\t\tip6 saddr @%s %s ct mark set %d accept comment \"%s\"\n", SetName(r.GetRuleId(), true), match.v6, mark, comment)
	}
	switch policy.GetMode() {
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED:
		fmt.Fprintf(&b, "\t\tlog prefix \"%s\" group %d drop comment \"innerwall terminal enforced\"\n", LogPrefix, opts.group())
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION:
		fmt.Fprintf(&b, "\t\tlog prefix \"%s\" group %d counter ct mark set 0x%08x accept comment \"innerwall terminal simulation\"\n", LogPrefix, opts.group(), WouldBlockMark)
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED:
		// Handled above.
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// RenderTeardown returns the script that deletes the owned table and
// nothing else: the local kill switch (ADR-0011).
func RenderTeardown(opts Options) string {
	return fmt.Sprintf("table inet %s {}\ndelete table inet %s\n", opts.table(), opts.table())
}

func writeSet(b *strings.Builder, name, kind string, elements []string) {
	fmt.Fprintf(b, "\tset %s {\n\t\ttype %s\n\t\tflags interval\n", name, kind)
	if len(elements) > 0 {
		fmt.Fprintf(b, "\t\telements = { %s }\n", strings.Join(elements, ", "))
	}
	b.WriteString("\t}\n")
}

// splitPeers partitions canonical prefixes by family. Prefixes that do
// not parse are dropped here: admission upstream never renders them, and
// a malformed element would fail the whole transaction.
func splitPeers(cidrs []string) (v4, v6 []string) {
	for _, c := range cidrs {
		switch {
		case strings.Contains(c, ":"):
			v6 = append(v6, c)
		case strings.Contains(c, "."):
			v4 = append(v4, c)
		}
	}
	sort.Strings(v4)
	sort.Strings(v6)
	return v4, v6
}

type l4 struct{ v4, v6 string }

// l4Match renders a rule's protocol and ports. No ports means every port
// of the protocol; ICMP has none and matches the family's ICMP protocol.
func l4Match(r *innerwallv1.ResolvedRule) l4 {
	switch r.GetProtocol() {
	case innerwallv1.Protocol_PROTOCOL_TCP:
		m := ports("tcp", r.GetPorts())
		return l4{m, m}
	case innerwallv1.Protocol_PROTOCOL_UDP:
		m := ports("udp", r.GetPorts())
		return l4{m, m}
	case innerwallv1.Protocol_PROTOCOL_ICMP:
		return l4{"meta l4proto icmp", "meta l4proto ipv6-icmp"}
	case innerwallv1.Protocol_PROTOCOL_UNSPECIFIED:
		fallthrough
	default:
		// Unrenderable protocol: a match no packet satisfies rather than
		// one every packet satisfies.
		return l4{"meta l4proto 255", "meta l4proto 255"}
	}
}

// ports renders a transport match: every port of the protocol when no
// range is given, else the listed ports and ranges.
func ports(proto string, ranges []*innerwallv1.PortRange) string {
	if len(ranges) == 0 {
		return "meta l4proto " + proto
	}
	parts := make([]string, 0, len(ranges))
	for _, p := range ranges {
		if p.GetStart() == p.GetEnd() {
			parts = append(parts, fmt.Sprint(p.GetStart()))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", p.GetStart(), p.GetEnd()))
		}
	}
	return proto + " dport { " + strings.Join(parts, ", ") + " }"
}
