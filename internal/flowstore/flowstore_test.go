package flowstore

import (
	"net/netip"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// TestAggregateTotalsMergesKeys checks the totals math: records sharing a
// totals key (two source addresses of one peer workload) add their
// counters, keep the earliest first-seen and latest last-seen, and take
// the snapshot fields from the latest-seen record; distinct keys stay
// apart and the result is ordered.
func TestAggregateTotalsMergesKeys(t *testing.T) {
	t0 := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	peer := Peer{Kind: PeerWorkload, Key: "w1", Labels: map[string]string{"role": "web"}}
	later := Peer{Kind: PeerWorkload, Key: "w1", Labels: map[string]string{"role": "web", "env": "prod"}}
	base := Record{DstPort: 5432, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED}
	a := base
	a.Peer, a.SrcAddress, a.ConnectionCount, a.ByteCount, a.FirstSeen, a.LastSeen = peer, netip.MustParseAddr("10.0.0.20"), 2, 200, t0.Add(10*time.Second), t0.Add(20*time.Second)
	b := base
	b.Peer, b.SrcAddress, b.ConnectionCount, b.ByteCount, b.FirstSeen, b.LastSeen = later, netip.MustParseAddr("fd00::20"), 3, 300, t0.Add(5*time.Second), t0.Add(30*time.Second)
	c := base
	c.Peer, c.DstPort, c.ConnectionCount, c.FirstSeen, c.LastSeen = Peer{Kind: PeerUnknown, Key: "192.0.2.1"}, 22, 1, t0, t0
	d := base
	d.Peer, d.Decision, d.ConnectionCount, d.FirstSeen, d.LastSeen = peer, innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK, 4, t0, t0

	out := AggregateTotals([]Record{c, a, d, b})
	if len(out) != 3 {
		t.Fatalf("totals = %d, want 3", len(out))
	}
	// Ordered by peer kind, then key, port, protocol, direction, decision.
	if out[0].Peer.Kind != PeerUnknown || out[0].DstPort != 22 || out[0].ConnectionCount != 1 {
		t.Fatalf("out[0] = %+v", out[0])
	}
	m := out[1]
	if m.Peer.Key != "w1" || m.Decision != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatalf("out[1] = %+v", m)
	}
	if m.ConnectionCount != 5 || m.ByteCount != 500 {
		t.Fatalf("merged counters = %d/%d", m.ConnectionCount, m.ByteCount)
	}
	if !m.FirstSeen.Equal(t0.Add(5*time.Second)) || !m.LastSeen.Equal(t0.Add(30*time.Second)) {
		t.Fatalf("merged seen = %v..%v", m.FirstSeen, m.LastSeen)
	}
	if m.Peer.Labels["env"] != "prod" || m.SrcAddress != netip.MustParseAddr("fd00::20") {
		t.Fatalf("snapshot fields not from the latest-seen record: %+v", m)
	}
	if out[2].Decision != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK || out[2].ConnectionCount != 4 {
		t.Fatalf("out[2] = %+v", out[2])
	}
	// Inputs are untouched.
	if a.ConnectionCount != 2 {
		t.Fatal("input mutated")
	}
	if len(AggregateTotals(nil)) != 0 {
		t.Fatal("nil input produced totals")
	}
}

func TestLabelsRoundTrip(t *testing.T) {
	if got := string(encodeLabels(nil)); got != "{}" {
		t.Fatalf("nil labels = %s", got)
	}
	b := encodeLabels(map[string]string{"role": "web", "env": "prod"})
	if string(b) != `{"env":"prod","role":"web"}` {
		t.Fatalf("encoded = %s", b)
	}
	got := decodeLabels(b)
	if got["role"] != "web" || got["env"] != "prod" {
		t.Fatalf("decoded = %v", got)
	}
	if got := decodeLabels([]byte("garbage")); len(got) != 0 {
		t.Fatalf("garbage decoded to %v", got)
	}
}
