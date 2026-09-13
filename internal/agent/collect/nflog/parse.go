// Package nflog observes the packets the terminal rule of the owned table
// logs: what the policy did not permit. Under enforcement each logged
// packet was dropped and becomes a BLOCKED observation; under simulation
// it was accepted and becomes a WOULD_BLOCK observation. Because a dropped
// packet never becomes a tracked connection, this is the only path by
// which blocked traffic reaches the flow map (ADR-0020).
package nflog

import (
	"encoding/binary"
	"errors"
	"net/netip"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Packet is what a logged packet's headers say about the flow it belongs
// to.
type Packet struct {
	Src      netip.Addr
	Dst      netip.Addr
	DstPort  uint16
	Protocol innerwallv1.Protocol
	Length   uint64
}

// ErrUnparseable is returned for a payload that is not an IPv4 or IPv6
// packet of a protocol the flow model knows.
var ErrUnparseable = errors.New("nflog: packet is not a parseable IPv4 or IPv6 packet")

// Protocol numbers as the kernel reports them.
const (
	protoICMP   = 1
	protoTCP    = 6
	protoUDP    = 17
	protoICMPv6 = 58
)

// Parse reads the network and transport headers of a logged packet. The
// payload starts at the IP header, as the input hook delivers it. IPv6
// extension headers are not walked: a packet carrying one is reported as
// unparseable rather than misattributed.
func Parse(payload []byte) (Packet, error) {
	if len(payload) < 1 {
		return Packet{}, ErrUnparseable
	}
	var p Packet
	var proto uint8
	var l4 []byte
	switch payload[0] >> 4 {
	case 4:
		if len(payload) < 20 {
			return Packet{}, ErrUnparseable
		}
		ihl := int(payload[0]&0x0f) * 4
		if ihl < 20 || len(payload) < ihl {
			return Packet{}, ErrUnparseable
		}
		proto = payload[9]
		p.Src = netip.AddrFrom4([4]byte(payload[12:16]))
		p.Dst = netip.AddrFrom4([4]byte(payload[16:20]))
		p.Length = uint64(binary.BigEndian.Uint16(payload[2:4]))
		l4 = payload[ihl:]
	case 6:
		if len(payload) < 40 {
			return Packet{}, ErrUnparseable
		}
		proto = payload[6]
		p.Src = netip.AddrFrom16([16]byte(payload[8:24]))
		p.Dst = netip.AddrFrom16([16]byte(payload[24:40]))
		p.Length = 40 + uint64(binary.BigEndian.Uint16(payload[4:6]))
		l4 = payload[40:]
	default:
		return Packet{}, ErrUnparseable
	}
	switch proto {
	case protoTCP:
		p.Protocol = innerwallv1.Protocol_PROTOCOL_TCP
	case protoUDP:
		p.Protocol = innerwallv1.Protocol_PROTOCOL_UDP
	case protoICMP, protoICMPv6:
		p.Protocol = innerwallv1.Protocol_PROTOCOL_ICMP
		return p, nil
	default:
		return Packet{}, ErrUnparseable
	}
	if len(l4) < 4 {
		return Packet{}, ErrUnparseable
	}
	p.DstPort = binary.BigEndian.Uint16(l4[2:4])
	return p, nil
}
