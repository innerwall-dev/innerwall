package storetest

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/ingest"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
	"github.com/innerwall-dev/innerwall/internal/store"
)

// An estate app is one label group of the review estate: its app label,
// how many workloads carry it, and the mode they run in.
type estateApp struct {
	app  string
	tier string
	n    int
	mode innerwallv1.EnforcementMode
}

// estateTraffic is one source-to-destination edge of the review estate:
// connections per workload pair per window on one port. The decision
// follows from the destination's mode: a visibility destination
// observes, a simulating one allows or would block, an enforced one
// allows or blocks, and whether a rule admits the source decides which.
type estateTraffic struct {
	src, dst string
	port     uint16
	conns    uint64
}

const (
	visibility = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY
	simulation = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION
	enforced   = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED
)

var estateApps = []estateApp{
	{"ci-runners", "infra", 6, visibility},
	{"storefront-web", "web", 24, visibility},
	{"bastion", "infra", 2, visibility},
	{"storefront-api", "api", 18, visibility},
	{"search", "api", 9, visibility},
	{"checkout", "api", 42, simulation},
	{"billing", "api", 16, visibility},
	{"auth", "api", 61, enforced},
	{"metrics-collector", "infra", 4, visibility},
}

// The unlabeled workloads, enrolled with no labels at all.
const estateUnlabeled = 30

var estateEdges = []estateTraffic{
	{"office", "bastion", 22, 600},
	{"office", "checkout", 22, 20},
	{"office", "unlabeled", 22, 44},
	{"corp-vpn", "checkout", 8443, 155},
	{"corp-vpn", "auth", 8443, 470},
	{"corp-vpn", "storefront-api", 8443, 1050},
	{"ci-runners", "search", 9200, 2200},
	{"ci-runners", "auth", 8443, 7},
	{"storefront-web", "storefront-api", 8443, 445_500},
	{"storefront-api", "checkout", 8443, 206_440},
	{"storefront-api", "auth", 8443, 116_500},
	{"storefront-api", "search", 9200, 60_000},
	{"storefront-api", "billing", 8443, 29_000},
	{"bastion", "checkout", 22, 44},
	{"bastion", "auth", 22, 30},
	{"bastion", "billing", 22, 15},
	{"checkout", "billing", 8443, 38_000},
	{"checkout", "auth", 8443, 45_500},
	{"unlabeled", "billing", 5432, 950},
	{"metrics-collector", "checkout", 9100, 9_102},
	{"metrics-collector", "auth", 9100, 13_500},
	{"metrics-collector", "billing", 9100, 5_500},
	{"metrics-collector", "search", 9100, 3_000},
	{"unknown", "checkout", 22, 1},
	{"unknown", "auth", 22, 4},
}

// estateAdmits is what the review estate's rulesets admit: which source
// may reach which destination, by the rule the flows name.
var estateAdmits = map[string][]string{
	"checkout": {"storefront-api", "bastion", "corp-vpn"},
	"auth":     {"storefront-api", "checkout", "bastion", "metrics-collector", "corp-vpn"},
}

// SeedEstate adds a review estate to a fleet Seed has loaded: the label
// groups, address groups, modes, and traffic of the flow map's design
// (a few hundred workloads under nine app labels plus thirty unlabeled
// ones, two address groups, and unknown sources), so the map and the
// fleet can be reviewed at the shape they were designed for. extra adds
// that many more app groups of three workloads each, every one reached
// by the storefront API, to exercise the map at estate scale. Every
// instant is relative to f.Now. It is development data only; no test
// asserts on its shape beyond that it loads.
func SeedEstate(ctx context.Context, s *store.Store, f *Fleet, extra int) error {
	if extra < 0 || extra > 700 {
		return fmt.Errorf("extra app groups %d: want 0 to 700", extra)
	}
	apps := append([]estateApp(nil), estateApps...)
	edges := append([]estateTraffic(nil), estateEdges...)
	for i := 1; i <= extra; i++ {
		name := fmt.Sprintf("svc-%03d", i)
		apps = append(apps, estateApp{name, "api", 3, visibility})
		edges = append(edges, estateTraffic{"storefront-api", name, 8443, 50 + 7*uint64(i)}) //nolint:gosec // i is in 1..700
	}

	members := map[string][]identity.WorkloadID{}
	addrs := map[identity.WorkloadID]netip.Addr{}
	enrolled := f.Now.Add(-20 * 24 * time.Hour)
	enrollOne := func(hostname string, labels []enroll.Label, addr netip.Addr) (identity.WorkloadID, error) {
		id, err := identity.NewWorkloadID()
		if err != nil {
			return id, err
		}
		w := enroll.Workload{ID: id, TokenID: f.Token.ID, Hostname: hostname, Labels: labels, EnrolledAt: enrolled, CredentialSerial: hostname + "-1", CredentialExpiresAt: f.Now.Add(18 * time.Hour)}
		if err := s.CreateWorkload(ctx, w, enrolled); err != nil {
			return id, err
		}
		facts := &innerwallv1.HostFacts{Hostname: hostname, Os: &innerwallv1.OsInfo{Family: "linux", Name: "debian", Version: "13", KernelVersion: "6.12", Architecture: "amd64"}, Interfaces: []*innerwallv1.NetworkInterface{{Name: "eth0", Addresses: []string{addr.String() + "/16"}}}}
		if _, err := s.RecordFacts(ctx, id, facts, enrolled); err != nil {
			return id, err
		}
		// Renewed six hours ago for a day, as the agent does in the last
		// third of each lifetime: the next renewal falls due in ten hours.
		renewed := f.Now.Add(-6 * time.Hour)
		if err := s.RecordRenewal(ctx, id, hostname+"-2", renewed.Add(24*time.Hour), renewed); err != nil {
			return id, err
		}
		addrs[id] = addr
		return id, nil
	}
	for ai, a := range apps {
		for i := 1; i <= a.n; i++ {
			// 10.64.0.0/10 holds the estate: one /16-ish block per app.
			addr := netip.AddrFrom4([4]byte{10, byte(64 + ai/4), byte(ai%4*64 + i/250), byte(i % 250)})
			labels := []enroll.Label{{Key: "app", Value: a.app}, {Key: "env", Value: "prod"}, {Key: "tier", Value: a.tier}}
			id, err := enrollOne(fmt.Sprintf("%s-prod-%02d", a.app, i), labels, addr)
			if err != nil {
				return err
			}
			if err := s.SetWorkloadMode(ctx, id, a.mode); err != nil {
				return err
			}
			members[a.app] = append(members[a.app], id)
		}
	}
	for i := 1; i <= estateUnlabeled; i++ {
		id, err := enrollOne(fmt.Sprintf("legacy-vm-%04d", 100+i), nil, netip.AddrFrom4([4]byte{10, 250, 0, byte(i)}))
		if err != nil {
			return err
		}
		members["unlabeled"] = append(members["unlabeled"], id)
	}

	engine := &compiler.Engine{Store: s}
	authoring := &policy.Authoring{Store: s, Renderer: renderer{engine}, Now: func() time.Time { return f.Now.Add(-6 * time.Hour) }}
	corpVPN := policy.AddressGroup{Name: "corp-vpn", CIDRs: []string{"10.40.0.0/16"}}
	if err := authoring.CreateAddressGroup(ctx, &corpVPN); err != nil {
		return err
	}
	// The seed's office group is 192.0.2.0/24; the estate's office traffic
	// comes from inside it.
	peerFor := func(src string) policy.Peer {
		if src == "corp-vpn" {
			return policy.Peer{Kind: policy.PeerAddressGroup, AddressGroupID: corpVPN.ID}
		}
		return policy.Peer{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"app": {src}}}
	}
	ruleFor := map[[2]string]string{}
	for _, dst := range []string{"checkout", "auth"} {
		rs := policy.Ruleset{Name: dst + "-inbound", Description: "what reaches " + dst, Enabled: true, Scope: policy.Selector{"app": {dst}}}
		for _, src := range estateAdmits[dst] {
			port := uint16(8443)
			if src == "bastion" {
				port = 22
			}
			if src == "metrics-collector" {
				port = 9100
			}
			rs.Rules = append(rs.Rules, policy.Rule{
				Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true, Description: src + " to " + dst,
				Peers:   []policy.Peer{peerFor(src)},
				Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: uint32(port), End: uint32(port)}}}},
			})
		}
		if err := authoring.CreateRuleset(ctx, &rs); err != nil {
			return err
		}
		for i, src := range estateAdmits[dst] {
			ruleFor[[2]string{src, dst}] = rendered.RuleID(rs.Rules[i].ID.String(), innerwallv1.Protocol_PROTOCOL_TCP)
		}
	}
	if _, err := engine.Render(ctx); err != nil {
		return err
	}

	// Every estate agent is connected and synced, except a few that
	// dropped flow records, so the map warns that it may be incomplete.
	droppers := map[identity.WorkloadID]bool{members["checkout"][30]: true, members["auth"][7]: true, members["billing"][2]: true}
	for _, ids := range members {
		for _, id := range ids {
			p, err := s.GetWorkloadPolicy(ctx, id)
			if err != nil {
				return err
			}
			v := p.GetVersion()
			seen := f.Now.Add(-20 * time.Second)
			if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "0.3.0", Capabilities: []string{"nftables", "conntrack"}}, v, seen); err != nil {
				return err
			}
			if err := s.RecordApplied(ctx, id, v, innerwallv1.SyncState_SYNC_STATE_SYNCED, seen); err != nil {
				return err
			}
			var dropped uint64
			if droppers[id] {
				dropped = 212
			}
			if err := s.RecordHeartbeat(ctx, id, dropped, "", seen); err != nil {
				return err
			}
		}
	}

	workloads, err := s.ListWorkloads(ctx)
	if err != nil {
		return err
	}
	groups, err := s.ListAddressGroups(ctx)
	if err != nil {
		return err
	}
	index := ingest.BuildIndex(workloads, groups)
	mode := map[string]innerwallv1.EnforcementMode{"unlabeled": visibility}
	for _, a := range apps {
		mode[a.app] = a.mode
	}
	// Sources that are not workloads: addresses inside the address
	// groups, and addresses no group holds.
	unmanaged := map[string][]netip.Addr{
		"office":   {netip.MustParseAddr("192.0.2.21"), netip.MustParseAddr("192.0.2.22")},
		"corp-vpn": {netip.MustParseAddr("10.40.3.7"), netip.MustParseAddr("10.40.9.12"), netip.MustParseAddr("10.40.12.4")},
		"unknown":  {netip.MustParseAddr("198.51.100.19"), netip.MustParseAddr("198.51.100.44"), netip.MustParseAddr("203.0.113.61")},
	}
	decision := func(src, dst string) (innerwallv1.PolicyDecision, string) {
		rule, admitted := ruleFor[[2]string{src, dst}]
		switch mode[dst] {
		case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED, visibility:
			return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ""
		case simulation:
			if admitted {
				return innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED, rule
			}
			return innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK, ""
		case enforced:
			if admitted {
				return innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED, rule
			}
			return innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED, ""
		default:
			return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ""
		}
	}
	for _, start := range []time.Time{f.Window1, f.Window2} {
		byDst := map[identity.WorkloadID][]flowstore.Record{}
		for _, e := range edges {
			d, rule := decision(e.src, e.dst)
			dsts := members[e.dst]
			// Each edge reaches a handful of the destination's workloads
			// from a handful of its sources, spread by the edge's index.
			var srcs []netip.Addr
			if u, ok := unmanaged[e.src]; ok {
				srcs = u
			} else {
				for _, id := range members[e.src] {
					srcs = append(srcs, addrs[id])
				}
			}
			for i := 0; i < min(4, len(dsts)); i++ {
				dst := dsts[(i*7)%len(dsts)]
				src := srcs[i%len(srcs)]
				conns := max(1, e.conns/uint64(min(4, len(dsts)))) //nolint:gosec // at most 4
				byDst[dst] = append(byDst[dst], flowstore.Record{
					Peer: index.Resolve(src), SrcAddress: src, DstAddress: addrs[dst], DstPort: e.port,
					Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: d,
					MatchedRuleID: rule, ConnectionCount: conns, ByteCount: conns * 900,
					FirstSeen: start.Add(10 * time.Second), LastSeen: start.Add(f.WindowLength - 10*time.Second),
				})
			}
		}
		for dst, records := range byDst {
			w := flowstore.Window{WorkloadID: dst, Start: start, End: start.Add(f.WindowLength), Records: records}
			if _, err := s.Flows().WriteWindow(ctx, w); err != nil {
				return err
			}
		}
	}
	return nil
}
