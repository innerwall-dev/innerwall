package rendered_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"google.golang.org/protobuf/proto"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

func rule(id string, p innerwallv1.Protocol, peers []string, ports ...uint32) *innerwallv1.ResolvedRule {
	r := &innerwallv1.ResolvedRule{RuleId: id, Protocol: p, PeerCidrs: peers}
	for i := 0; i+1 < len(ports); i += 2 {
		r.Ports = append(r.Ports, &innerwallv1.PortRange{Start: ports[i], End: ports[i+1]})
	}
	return r
}

func policy(version uint64, mode innerwallv1.EnforcementMode, rules ...*innerwallv1.ResolvedRule) *innerwallv1.WorkloadPolicy {
	return &innerwallv1.WorkloadPolicy{Version: version, Mode: mode, InboundRules: rules}
}

const (
	vis = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY
	sim = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION
	enf = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED
	tcp = innerwallv1.Protocol_PROTOCOL_TCP
	udp = innerwallv1.Protocol_PROTOCOL_UDP
)

// variantOf names the oneof case of a change, for coverage accounting.
func variantOf(ch *innerwallv1.DeltaChange) string {
	switch ch.GetChange().(type) {
	case *innerwallv1.DeltaChange_UpsertRule:
		return "upsert_rule"
	case *innerwallv1.DeltaChange_RemoveRuleId:
		return "remove_rule_id"
	case *innerwallv1.DeltaChange_AddPeers:
		return "add_peers"
	case *innerwallv1.DeltaChange_RemovePeers:
		return "remove_peers"
	case *innerwallv1.DeltaChange_SetMode:
		return "set_mode"
	default:
		return "unknown"
	}
}

// allVariants is every DeltaChange case the wire contract defines. The
// property test below fails if the table stops exercising any of them.
var allVariants = []string{"upsert_rule", "remove_rule_id", "add_peers", "remove_peers", "set_mode"}

// checkRoundTrip asserts the delta contract for one pair of consecutive
// versions and returns the variants the delta used.
func checkRoundTrip(t *testing.T, from, to *innerwallv1.WorkloadPolicy) map[string]int {
	t.Helper()
	delta := rendered.Delta(from, to)
	if delta.GetFromVersion() != from.GetVersion() || delta.GetToVersion() != to.GetVersion() {
		t.Fatalf("delta versions %d->%d, want %d->%d", delta.GetFromVersion(), delta.GetToVersion(), from.GetVersion(), to.GetVersion())
	}
	got, err := rendered.Apply(from, delta)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	want := rendered.Canonical(to)
	if !proto.Equal(got, want) {
		t.Fatalf("apply(snapshot(v%d), delta) != snapshot(v%d)\n got: %v\nwant: %v\ndelta: %v", from.GetVersion(), to.GetVersion(), got, want, delta)
	}
	// Exact serialization equality, not just proto.Equal: the persisted
	// bytes of the two must match.
	gb, _ := rendered.Marshal(got)
	wb, _ := rendered.Marshal(want)
	if string(gb) != string(wb) {
		t.Fatal("canonical serializations differ")
	}
	// Idempotence of Diff: a second diff between the result and the
	// target is empty.
	if again := rendered.Diff(got, to); len(again) != 0 {
		t.Fatalf("diff after apply is not empty: %v", again)
	}
	// Minimality: the number of changes equals what the shapes require;
	// checked by the caller through the variant counts.
	counts := map[string]int{}
	for _, ch := range delta.GetChanges() {
		counts[variantOf(ch)]++
	}
	// Equal(from,to) iff no changes.
	if rendered.Equal(from, to) != (len(delta.GetChanges()) == 0) {
		t.Fatalf("Equal=%v but %d changes", rendered.Equal(from, to), len(delta.GetChanges()))
	}
	return counts
}

// TestSnapshotDeltaProperty is the executable statement of the delta
// contract: for every change shape, applying the delta between two
// consecutive rendered versions to the earlier snapshot yields the later
// snapshot exactly. The table covers each DeltaChange variant on its own
// and in combination; the randomized section then exercises arbitrary
// pairs.
func TestSnapshotDeltaProperty(t *testing.T) {
	a := "r1/tcp"
	b := "r2/tcp"
	base := policy(1, vis,
		rule(a, tcp, []string{"10.0.0.1/32", "10.0.0.2/32"}, 5432, 5432),
		rule(b, tcp, []string{"10.1.0.0/16"}, 80, 80, 443, 443),
	)
	cases := []struct {
		name string
		from *innerwallv1.WorkloadPolicy
		to   *innerwallv1.WorkloadPolicy
		want map[string]int // exact variant counts expected
	}{
		{
			name: "no-op",
			from: base,
			to:   policy(1, vis, rule(b, tcp, []string{"10.1.0.0/16"}, 443, 443, 80, 80), rule(a, tcp, []string{"10.0.0.2/32", "10.0.0.1/32"}, 5432, 5432)),
			want: map[string]int{},
		},
		{
			name: "rule add",
			from: base,
			to:   policy(2, vis, base.InboundRules[0], base.InboundRules[1], rule("r3/udp", udp, []string{"10.2.0.0/24"}, 53, 53)),
			want: map[string]int{"upsert_rule": 1},
		},
		{
			name: "rule remove",
			from: base,
			to:   policy(2, vis, base.InboundRules[1]),
			want: map[string]int{"remove_rule_id": 1},
		},
		{
			name: "peer added",
			from: base,
			to:   policy(2, vis, rule(a, tcp, []string{"10.0.0.1/32", "10.0.0.2/32", "10.0.0.3/32"}, 5432, 5432), base.InboundRules[1]),
			want: map[string]int{"add_peers": 1},
		},
		{
			name: "peer removed",
			from: base,
			to:   policy(2, vis, rule(a, tcp, []string{"10.0.0.2/32"}, 5432, 5432), base.InboundRules[1]),
			want: map[string]int{"remove_peers": 1},
		},
		{
			name: "peer membership churn (one in, one out)",
			from: base,
			to:   policy(2, vis, rule(a, tcp, []string{"10.0.0.2/32", "10.0.0.9/32"}, 5432, 5432), base.InboundRules[1]),
			want: map[string]int{"add_peers": 1, "remove_peers": 1},
		},
		{
			name: "ports changed is a wholesale upsert",
			from: base,
			to:   policy(2, vis, base.InboundRules[0], rule(b, tcp, []string{"10.1.0.0/16"}, 80, 80)),
			want: map[string]int{"upsert_rule": 1},
		},
		{
			name: "protocol changed is a wholesale upsert",
			from: base,
			to:   policy(2, vis, base.InboundRules[0], rule(b, udp, []string{"10.1.0.0/16"}, 80, 80, 443, 443)),
			want: map[string]int{"upsert_rule": 1},
		},
		{
			name: "mode flip only",
			from: base,
			to:   policy(2, enf, base.InboundRules...),
			want: map[string]int{"set_mode": 1},
		},
		{
			name: "everything at once",
			from: base,
			to: policy(2, sim,
				rule(a, tcp, []string{"10.0.0.2/32", "192.168.0.0/24"}, 5432, 5432), // peers churn
				rule("r9/tcp", tcp, []string{"0.0.0.0/0"}, 22, 22),                  // added; r2 removed
			),
			want: map[string]int{"set_mode": 1, "add_peers": 1, "remove_peers": 1, "upsert_rule": 1, "remove_rule_id": 1},
		},
		{
			name: "from empty (first snapshot as a delta)",
			from: rendered.Empty(),
			to:   base,
			want: map[string]int{"upsert_rule": 2},
		},
		{
			name: "to empty",
			from: base,
			to:   policy(2, vis),
			want: map[string]int{"remove_rule_id": 2},
		},
		{
			name: "all peers removed leaves an empty rule, not a removed rule",
			from: base,
			to:   policy(2, vis, rule(a, tcp, nil, 5432, 5432), base.InboundRules[1]),
			want: map[string]int{"remove_peers": 1},
		},
		{
			name: "uncanonical input: duplicate peers and bare addresses",
			from: base,
			to:   policy(2, vis, rule(a, tcp, []string{"10.0.0.1", "10.0.0.1/32", "10.0.0.2/32", "10.0.0.2"}, 5432, 5432), base.InboundRules[1]),
			want: map[string]int{},
		},
	}
	covered := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkRoundTrip(t, tc.from, tc.to)
			for v, n := range tc.want {
				if got[v] != n {
					t.Errorf("variant %s: %d changes, want %d (delta %v)", v, got[v], n, rendered.Diff(tc.from, tc.to))
				}
			}
			for v, n := range got {
				if tc.want[v] != n {
					t.Errorf("unexpected variant %s x%d", v, n)
				}
				covered[v] = true
			}
		})
	}
	for _, v := range allVariants {
		if !covered[v] {
			t.Errorf("DeltaChange variant %s is not covered by the table", v)
		}
	}

	t.Run("randomized", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // deterministic test data
		seen := map[string]bool{}
		for i := range 2000 {
			from := randomPolicy(rng, uint64(i)) //nolint:gosec // loop index
			to := mutate(rng, from)
			for v := range checkRoundTrip(t, from, to) {
				seen[v] = true
			}
		}
		for _, v := range allVariants {
			if !seen[v] {
				t.Errorf("randomized pairs never produced %s", v)
			}
		}
	})
}

var peerPool = []string{"10.0.0.1/32", "10.0.0.2/32", "10.0.0.3/32", "fd00::1/128", "192.168.1.0/24", "10.0.0.0/8"}
var modes = []innerwallv1.EnforcementMode{vis, sim, enf}
var protos = []innerwallv1.Protocol{tcp, udp, innerwallv1.Protocol_PROTOCOL_ICMP}

func randomRule(rng *rand.Rand, i int) *innerwallv1.ResolvedRule {
	p := protos[rng.IntN(len(protos))]
	r := &innerwallv1.ResolvedRule{RuleId: rendered.RuleID(fmt.Sprintf("rule-%d", i), p), Protocol: p}
	for _, peer := range peerPool {
		if rng.IntN(2) == 0 {
			r.PeerCidrs = append(r.PeerCidrs, peer)
		}
	}
	if p != innerwallv1.Protocol_PROTOCOL_ICMP {
		for range rng.IntN(3) {
			start := uint32(rng.IntN(1000))                                                                    //nolint:gosec // small
			r.Ports = append(r.Ports, &innerwallv1.PortRange{Start: start, End: start + uint32(rng.IntN(10))}) //nolint:gosec // small
		}
	}
	return r
}

func randomPolicy(rng *rand.Rand, version uint64) *innerwallv1.WorkloadPolicy {
	p := policy(version, modes[rng.IntN(len(modes))])
	for i := range rng.IntN(5) {
		p.InboundRules = append(p.InboundRules, randomRule(rng, i))
	}
	return p
}

// mutate applies a few random authored-style edits to a copy of p and bumps
// the version, producing a plausible next rendered version.
func mutate(rng *rand.Rand, p *innerwallv1.WorkloadPolicy) *innerwallv1.WorkloadPolicy {
	out := proto.Clone(p).(*innerwallv1.WorkloadPolicy)
	out.Version = p.GetVersion() + 1
	for range 1 + rng.IntN(3) {
		switch rng.IntN(5) {
		case 0:
			out.Mode = modes[rng.IntN(len(modes))]
		case 1:
			out.InboundRules = append(out.InboundRules, randomRule(rng, 10+rng.IntN(4)))
		case 2:
			if n := len(out.InboundRules); n > 0 {
				i := rng.IntN(n)
				out.InboundRules = append(out.InboundRules[:i], out.InboundRules[i+1:]...)
			}
		case 3:
			if n := len(out.InboundRules); n > 0 {
				r := out.InboundRules[rng.IntN(n)]
				r.PeerCidrs = append(r.PeerCidrs, peerPool[rng.IntN(len(peerPool))])
			}
		case 4:
			if n := len(out.InboundRules); n > 0 {
				r := out.InboundRules[rng.IntN(n)]
				if len(r.PeerCidrs) > 0 {
					r.PeerCidrs = r.PeerCidrs[1:]
				}
			}
		}
	}
	return out
}

func TestApplyRejectsWrongBase(t *testing.T) {
	from := policy(3, vis, rule("r/tcp", tcp, []string{"10.0.0.1/32"}, 22, 22))
	to := policy(4, vis)
	delta := rendered.Delta(from, to)
	// An agent on version 2 must not apply a delta from 3.
	if _, err := rendered.Apply(policy(2, vis), delta); !errors.Is(err, rendered.ErrBaseMismatch) {
		t.Fatalf("err = %v, want ErrBaseMismatch", err)
	}
	// And a peer change naming an unknown rule fails without side effects.
	bad := &innerwallv1.PolicyDelta{FromVersion: 3, ToVersion: 4, Changes: []*innerwallv1.DeltaChange{
		{Change: &innerwallv1.DeltaChange_AddPeers{AddPeers: &innerwallv1.PeerChange{RuleId: "ghost/tcp", PeerCidrs: []string{"10.9.9.9/32"}}}},
	}}
	before := proto.Clone(from)
	if _, err := rendered.Apply(from, bad); !errors.Is(err, rendered.ErrUnknownRule) {
		t.Fatalf("err = %v, want ErrUnknownRule", err)
	}
	if !proto.Equal(before, from) {
		t.Fatal("failed apply modified the base policy")
	}
}

func TestRuleIDProvenance(t *testing.T) {
	id := rendered.RuleID("0192f3a0-0000-7000-8000-000000000001", udp)
	if id != "0192f3a0-0000-7000-8000-000000000001/udp" {
		t.Fatalf("id = %q", id)
	}
	authored, p, err := rendered.Provenance(id)
	if err != nil || authored != "0192f3a0-0000-7000-8000-000000000001" || p != udp {
		t.Fatalf("provenance = %q %v %v", authored, p, err)
	}
	for _, bad := range []string{"", "x", "x/", "/tcp", "x/gopher"} {
		if _, _, err := rendered.Provenance(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestNormalizeCIDR(t *testing.T) {
	cases := []struct{ in, want string }{
		{"10.0.4.17/24", "10.0.4.0/24"},
		{"10.0.4.17", "10.0.4.17/32"},
		{" fd00::1 ", "fd00::1/128"}, // surrounding whitespace is trimmed
		{"::ffff:10.0.0.1", "10.0.0.1/32"},
		{"garbage", "garbage"},
	}
	for _, tc := range cases {
		if got := rendered.NormalizeCIDR(tc.in); got != tc.want {
			t.Errorf("NormalizeCIDR(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
