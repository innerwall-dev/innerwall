package nflog

import (
	"encoding/binary"
	"net/netip"
	"testing"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

func ipv4(proto byte, dport uint16, total uint16) []byte {
	const src, dst = "10.0.0.20", "10.0.0.10"
	b := make([]byte, 20+8)
	b[0] = 0x45
	binary.BigEndian.PutUint16(b[2:4], total)
	b[9] = proto
	copy(b[12:16], netip.MustParseAddr(src).AsSlice())
	copy(b[16:20], netip.MustParseAddr(dst).AsSlice())
	binary.BigEndian.PutUint16(b[20:22], 51234)
	binary.BigEndian.PutUint16(b[22:24], dport)
	return b
}

func ipv6(next byte, dport uint16, payloadLen uint16) []byte {
	const src, dst = "fd00::20", "fd00::10"
	b := make([]byte, 40+8)
	b[0] = 0x60
	binary.BigEndian.PutUint16(b[4:6], payloadLen)
	b[6] = next
	copy(b[8:24], netip.MustParseAddr(src).AsSlice())
	copy(b[24:40], netip.MustParseAddr(dst).AsSlice())
	binary.BigEndian.PutUint16(b[40:42], 51234)
	binary.BigEndian.PutUint16(b[42:44], dport)
	return b
}

func TestParse(t *testing.T) {
	p, err := Parse(ipv4(protoTCP, 9090, 60))
	if err != nil || p.Src != netip.MustParseAddr("10.0.0.20") || p.Dst != netip.MustParseAddr("10.0.0.10") || p.DstPort != 9090 || p.Protocol != innerwallv1.Protocol_PROTOCOL_TCP || p.Length != 60 {
		t.Fatalf("v4 tcp = %+v, %v", p, err)
	}
	p, err = Parse(ipv4(protoUDP, 53, 40))
	if err != nil || p.DstPort != 53 || p.Protocol != innerwallv1.Protocol_PROTOCOL_UDP {
		t.Fatalf("v4 udp = %+v, %v", p, err)
	}
	p, err = Parse(ipv4(protoICMP, 0, 84))
	if err != nil || p.DstPort != 0 || p.Protocol != innerwallv1.Protocol_PROTOCOL_ICMP || p.Length != 84 {
		t.Fatalf("v4 icmp = %+v, %v", p, err)
	}
	p, err = Parse(ipv6(protoTCP, 443, 20))
	if err != nil || p.Src != netip.MustParseAddr("fd00::20") || p.DstPort != 443 || p.Length != 60 {
		t.Fatalf("v6 tcp = %+v, %v", p, err)
	}
	p, err = Parse(ipv6(protoICMPv6, 0, 8))
	if err != nil || p.Protocol != innerwallv1.Protocol_PROTOCOL_ICMP {
		t.Fatalf("v6 icmp = %+v, %v", p, err)
	}
	for name, bad := range map[string][]byte{
		"empty":         {},
		"short v4":      ipv4(protoTCP, 1, 1)[:19],
		"v4 no l4":      ipv4(protoTCP, 1, 1)[:22],
		"other proto":   ipv4(47, 0, 40),
		"v6 extension":  ipv6(0, 1, 8),
		"not ip":        {0x12, 0x34},
		"bad ihl":       append([]byte{0x43}, ipv4(protoTCP, 1, 1)[1:]...),
		"short v6":      ipv6(protoTCP, 1, 1)[:39],
		"v6 short l4":   ipv6(protoTCP, 1, 1)[:42],
		"unknown v6 nh": ipv6(47, 1, 8),
	} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("%s parsed", name)
		}
	}
}
