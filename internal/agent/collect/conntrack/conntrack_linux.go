//go:build linux

// Package conntrack observes inbound connections through the kernel's
// connection tracking events over netlink (ADR-0003): a NEW event counts a
// connection, a DESTROY event attributes its bytes. Only connections whose
// original destination is one of this host's addresses are inbound and
// reported (ADR-0010); everything else the kernel tracks (forwarded,
// outbound, loopback) is ignored at the source.
package conntrack

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	ct "github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Protocol numbers as the kernel reports them.
const (
	protoICMP   = 1
	protoTCP    = 6
	protoUDP    = 17
	protoICMPv6 = 58
)

// acctPath is where the kernel says whether it accounts bytes per
// connection. Without it every DESTROY event carries zero counters and
// byte totals are reported as zero; the operator enables it with a
// sysctl, and the source says so once at start.
const acctPath = "/proc/sys/net/netfilter/nf_conntrack_acct"

// eventQueue bounds events waiting to be classified.
const eventQueue = 4096

// localRefresh is how often the host's own addresses are re-read.
const localRefresh = 30 * time.Second

// Source is the conntrack event source.
type Source struct {
	Log *slog.Logger
	// Classify maps a connection mark to the decision and rule id the
	// observation carries. Nil classifies everything as OBSERVED with no
	// rule, which is visibility mode.
	Classify func(mark uint32) (innerwallv1.PolicyDecision, string)
	// LocalAddresses returns the host's own addresses; net.InterfaceAddrs
	// when nil.
	LocalAddresses func() []netip.Addr

	mu    sync.RWMutex
	local map[netip.Addr]struct{}
}

var _ collect.Source = (*Source)(nil)

func (s *Source) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Run implements collect.Source.
func (s *Source) Run(ctx context.Context, emit func(collect.Observation)) error {
	conn, err := ct.Dial(nil)
	if err != nil {
		return fmt.Errorf("conntrack: opening netlink: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if b, err := os.ReadFile(acctPath); err == nil && strings.TrimSpace(string(b)) != "1" {
		s.log().Warn("connection byte accounting is off; flow byte counts will be zero", "sysctl", "net.netfilter.nf_conntrack_acct")
	}
	s.refreshLocal()

	events := make(chan ct.Event, eventQueue)
	errs, err := conn.Listen(events, 1, []netfilter.NetlinkGroup{netfilter.GroupCTNew, netfilter.GroupCTDestroy})
	if err != nil {
		return fmt.Errorf("conntrack: subscribing to events: %w", err)
	}
	s.log().Info("conntrack event source started")
	refresh := time.NewTicker(localRefresh)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			return fmt.Errorf("conntrack: event stream: %w", err)
		case <-refresh.C:
			s.refreshLocal()
		case ev := <-events:
			if o, ok := s.classify(ev, time.Now()); ok {
				emit(o)
			}
		}
	}
}

// classify turns one event into an observation, or nothing when the event
// is not an inbound connection to this host.
func (s *Source) classify(ev ct.Event, now time.Time) (collect.Observation, bool) {
	f := ev.Flow
	if f == nil {
		return collect.Observation{}, false
	}
	src := f.TupleOrig.IP.SourceAddress.Unmap()
	dst := f.TupleOrig.IP.DestinationAddress.Unmap()
	if !src.IsValid() || !dst.IsValid() || src.IsLoopback() || dst.IsLoopback() {
		return collect.Observation{}, false
	}
	if !s.isLocal(dst) {
		return collect.Observation{}, false
	}
	var proto innerwallv1.Protocol
	var port uint16
	switch f.TupleOrig.Proto.Protocol {
	case protoTCP:
		proto, port = innerwallv1.Protocol_PROTOCOL_TCP, f.TupleOrig.Proto.DestinationPort
	case protoUDP:
		proto, port = innerwallv1.Protocol_PROTOCOL_UDP, f.TupleOrig.Proto.DestinationPort
	case protoICMP, protoICMPv6:
		proto = innerwallv1.Protocol_PROTOCOL_ICMP
	default:
		return collect.Observation{}, false
	}
	o := collect.Observation{At: now, Src: src, Dst: dst, DstPort: port, Protocol: proto, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED}
	if s.Classify != nil {
		o.Decision, o.RuleID = s.Classify(f.Mark)
	}
	switch ev.Type {
	case ct.EventNew:
		if o.Decision == innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
			// Attempts that simulation would have dropped are counted
			// by the log path, one per packet, exactly as enforcement
			// counts drops; conntrack contributes their bytes only.
			return collect.Observation{}, false
		}
		o.Connections = 1
	case ct.EventDestroy:
		o.Bytes = f.CountersOrig.Bytes + f.CountersReply.Bytes
	case ct.EventUnknown, ct.EventUpdate, ct.EventExpNew, ct.EventExpDestroy:
		// Updates are not subscribed to and expectations are not flows.
		return collect.Observation{}, false
	default:
		return collect.Observation{}, false
	}
	return o, true
}

func (s *Source) isLocal(a netip.Addr) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.local[a]
	return ok
}

func (s *Source) refreshLocal() {
	var addrs []netip.Addr
	if s.LocalAddresses != nil {
		addrs = s.LocalAddresses()
	} else {
		addrs = interfaceAddresses()
	}
	local := make(map[netip.Addr]struct{}, len(addrs))
	for _, a := range addrs {
		local[a.Unmap()] = struct{}{}
	}
	s.mu.Lock()
	s.local = local
	s.mu.Unlock()
}

func interfaceAddresses() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range addrs {
		if pfx, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, pfx.Addr())
		} else if addr, err := netip.ParseAddr(a.String()); err == nil {
			out = append(out, addr)
		}
	}
	return out
}
