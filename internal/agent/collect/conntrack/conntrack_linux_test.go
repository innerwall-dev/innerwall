//go:build linux

package conntrack

import (
	"net/netip"
	"testing"
	"time"

	ct "github.com/ti-mo/conntrack"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

func flow(src, dst string, proto uint8, dport uint16, mark uint32, bytesOrig, bytesReply uint64) *ct.Flow {
	f := &ct.Flow{Mark: mark}
	f.TupleOrig.IP.SourceAddress = netip.MustParseAddr(src)
	f.TupleOrig.IP.DestinationAddress = netip.MustParseAddr(dst)
	f.TupleOrig.Proto.Protocol = proto
	f.TupleOrig.Proto.DestinationPort = dport
	f.TupleOrig.Proto.SourcePort = 51234
	f.CountersOrig.Bytes = bytesOrig
	f.CountersReply.Bytes = bytesReply
	return f
}

// TestClassifyEvents checks the conntrack boundary without netlink: a NEW
// event to a local address is one connection on the destination port
// (never the ephemeral source port), a DESTROY event carries the bytes of
// both directions, ICMP has no port, and anything not inbound to this host
// (forwarded, outbound, loopback, an unknown protocol) is dropped at the
// source. The mark is handed to the classifier.
func TestClassifyEvents(t *testing.T) {
	s := &Source{LocalAddresses: func() []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("10.0.0.10"), netip.MustParseAddr("fd00::10")}
	}}
	s.refreshLocal()
	now := time.Now()

	o, ok := s.classify(ct.Event{Type: ct.EventNew, Flow: flow("10.0.0.20", "10.0.0.10", protoTCP, 5432, 0, 0, 0)}, now)
	if !ok || o.Connections != 1 || o.Bytes != 0 || o.DstPort != 5432 || o.Protocol != innerwallv1.Protocol_PROTOCOL_TCP || o.Decision != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED || o.RuleID != "" {
		t.Fatalf("new tcp = %+v, %v", o, ok)
	}
	if o.Src != netip.MustParseAddr("10.0.0.20") || o.Dst != netip.MustParseAddr("10.0.0.10") || !o.At.Equal(now) {
		t.Fatalf("new tcp endpoints = %+v", o)
	}
	o, ok = s.classify(ct.Event{Type: ct.EventDestroy, Flow: flow("10.0.0.20", "10.0.0.10", protoUDP, 53, 0, 300, 700)}, now)
	if !ok || o.Connections != 0 || o.Bytes != 1000 || o.Protocol != innerwallv1.Protocol_PROTOCOL_UDP {
		t.Fatalf("destroy udp = %+v, %v", o, ok)
	}
	o, ok = s.classify(ct.Event{Type: ct.EventNew, Flow: flow("fd00::20", "fd00::10", protoICMPv6, 0, 0, 0, 0)}, now)
	if !ok || o.Protocol != innerwallv1.Protocol_PROTOCOL_ICMP || o.DstPort != 0 {
		t.Fatalf("icmpv6 = %+v, %v", o, ok)
	}
	// Mapped addresses are unmapped before the local check.
	if _, ok := s.classify(ct.Event{Type: ct.EventNew, Flow: flow("::ffff:10.0.0.20", "::ffff:10.0.0.10", protoTCP, 22, 0, 0, 0)}, now); !ok {
		t.Fatal("mapped local destination not recognized")
	}

	drops := []struct {
		name string
		ev   ct.Event
	}{
		{"outbound", ct.Event{Type: ct.EventNew, Flow: flow("10.0.0.10", "10.0.0.20", protoTCP, 443, 0, 0, 0)}},
		{"forwarded", ct.Event{Type: ct.EventNew, Flow: flow("10.0.0.20", "10.0.0.30", protoTCP, 443, 0, 0, 0)}},
		{"loopback", ct.Event{Type: ct.EventNew, Flow: flow("127.0.0.1", "127.0.0.1", protoTCP, 5432, 0, 0, 0)}},
		{"unknown protocol", ct.Event{Type: ct.EventNew, Flow: flow("10.0.0.20", "10.0.0.10", 47, 0, 0, 0, 0)}},
		{"update", ct.Event{Type: ct.EventUpdate, Flow: flow("10.0.0.20", "10.0.0.10", protoTCP, 5432, 0, 0, 0)}},
		{"no flow", ct.Event{Type: ct.EventNew}},
	}
	for _, d := range drops {
		if _, ok := s.classify(d.ev, now); ok {
			t.Fatalf("%s was not dropped", d.name)
		}
	}

	// The mark reaches the classifier, which decides the decision and rule.
	s.Classify = func(mark uint32) (innerwallv1.PolicyDecision, string) {
		if mark == 7 {
			return innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED, "rule-7/tcp"
		}
		return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ""
	}
	o, _ = s.classify(ct.Event{Type: ct.EventNew, Flow: flow("10.0.0.20", "10.0.0.10", protoTCP, 5432, 7, 0, 0)}, now)
	if o.Decision != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || o.RuleID != "rule-7/tcp" {
		t.Fatalf("marked = %+v", o)
	}
}
