//go:build linux

package conntrack

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	ct "github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"
	"golang.org/x/sys/unix"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
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

// classify observes one event on a subscription whose dump is done.
func (s *Source) classify(ev ct.Event, now time.Time) (collect.Observation, bool) {
	t := newSightings(100)
	t.dumping = false
	return s.observe(t, ev, now, now)
}

func localSource() *Source {
	s := &Source{LocalAddresses: func() []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("10.0.0.10"), netip.MustParseAddr("fd00::10")}
	}}
	s.refreshLocal()
	return s
}

// TestClassifyEvents checks the conntrack boundary without netlink: a NEW
// event to a local address is one connection on the destination port
// (never the ephemeral source port), a DESTROY event carries the bytes of
// both directions, ICMP has no port, and anything not inbound to this host
// (forwarded, outbound, loopback, an unknown protocol) is dropped at the
// source. The mark is handed to the classifier.
func TestClassifyEvents(t *testing.T) {
	s := localSource()
	now := time.Now()

	o, ok := s.classify(ct.Event{Type: ct.EventNew, Flow: flow("10.0.0.20", "10.0.0.10", protoTCP, 5432, 0, 0, 0)}, now)
	if !ok || o.Connections != 1 || o.Bytes != 0 || o.DstPort != 5432 || o.Protocol != innerwallv1.Protocol_PROTOCOL_TCP || o.Decision != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED || o.RuleID != "" {
		t.Fatalf("new tcp = %+v, %v", o, ok)
	}
	if o.Src != netip.MustParseAddr("10.0.0.20") || o.Dst != netip.MustParseAddr("10.0.0.10") || !o.At.Equal(now) {
		t.Fatalf("new tcp endpoints = %+v", o)
	}
	// A close the subscription never sighted is found at close: counted.
	o, ok = s.classify(ct.Event{Type: ct.EventDestroy, Flow: flow("10.0.0.20", "10.0.0.10", protoUDP, 53, 0, 300, 700)}, now)
	if !ok || o.Connections != 1 || o.Bytes != 1000 || o.Protocol != innerwallv1.Protocol_PROTOCOL_UDP {
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

func withID(f *ct.Flow, id uint32) *ct.Flow {
	f.ID = id
	return f
}

// wouldBlockMark is what classifyMarks treats as the would-block mark.
const wouldBlockMark = 0xffff0000

func classifyMarks(mark uint32) (innerwallv1.PolicyDecision, string) {
	if mark == wouldBlockMark {
		return innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK, ""
	}
	return innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ""
}

type emitted struct {
	mu  sync.Mutex
	all []collect.Observation
}

func (e *emitted) add(o collect.Observation) {
	e.mu.Lock()
	e.all = append(e.all, o)
	e.mu.Unlock()
}

func (e *emitted) take() []collect.Observation {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.all
	e.all = nil
	return out
}

// TestEachConnectionCountedOnce checks the counting semantics around the
// dump: a connection sighted by a NEW event is not counted again by the
// dump, a connection closing while the dump is in flight is counted at
// its close and not again by the dump, the dump counts the rest through
// the same filter and classification as a NEW event (would-block
// connections excepted, as the log path counts them), and a close is
// bytes only for a connection already counted and counted once for one
// never sighted, so no record carries bytes without a connection.
func TestEachConnectionCountedOnce(t *testing.T) {
	s := localSource()
	s.Classify = classifyMarks
	subscribed := time.Now()
	now := subscribed.Add(time.Millisecond)
	tr := newSightings(100)

	o, ok := s.observe(tr, ct.Event{Type: ct.EventNew, Flow: withID(flow("10.0.0.20", "10.0.0.10", protoTCP, 5432, 0, 0, 0), 1)}, now, subscribed)
	if !ok || o.Connections != 1 {
		t.Fatalf("new = %+v, %v", o, ok)
	}
	// Opened and closed while the dump is in flight: counted by its NEW,
	// bytes only at its close, and not again if the dump still lists it.
	if o, ok := s.observe(tr, ct.Event{Type: ct.EventNew, Flow: withID(flow("10.0.0.26", "10.0.0.10", protoTCP, 9000, 0, 0, 0), 30)}, now, subscribed); !ok || o.Connections != 1 {
		t.Fatalf("short-lived new = %+v, %v", o, ok)
	}
	if o, ok := s.observe(tr, ct.Event{Type: ct.EventDestroy, Flow: withID(flow("10.0.0.26", "10.0.0.10", protoTCP, 9000, 0, 2, 2), 30)}, now, subscribed); !ok || o.Connections != 0 {
		t.Fatalf("short-lived close = %+v, %v", o, ok)
	}
	o, ok = s.observe(tr, ct.Event{Type: ct.EventDestroy, Flow: withID(flow("10.0.0.21", "10.0.0.10", protoTCP, 5432, 0, 10, 20), 9)}, now, subscribed)
	if !ok || o.Connections != 1 || o.Bytes != 30 {
		t.Fatalf("close during the dump = %+v, %v", o, ok)
	}
	if _, ok := s.observe(tr, ct.Event{Type: ct.EventNew, Flow: withID(flow("10.0.0.22", "10.0.0.10", protoTCP, 80, wouldBlockMark, 0, 0), 6)}, now, subscribed); ok {
		t.Fatal("a would-block NEW was counted by conntrack")
	}

	var e emitted
	s.fold(tr, []ct.Flow{
		*withID(flow("10.0.0.20", "10.0.0.10", protoTCP, 5432, 0, 0, 0), 1),            // sighted by NEW
		*withID(flow("10.0.0.21", "10.0.0.10", protoTCP, 5432, 0, 0, 0), 9),            // closed during the dump
		*withID(flow("10.0.0.23", "10.0.0.10", protoTCP, 6379, 0, 500, 500), 3),        // live, unseen
		*withID(flow("10.0.0.10", "10.0.0.30", protoTCP, 443, 0, 0, 0), 4),             // outbound
		*withID(flow("10.0.0.22", "10.0.0.10", protoTCP, 80, wouldBlockMark, 0, 0), 5), // would-block
		*withID(flow("127.0.0.1", "127.0.0.1", protoTCP, 5432, 0, 0, 0), 7),            // loopback
		*withID(flow("10.0.0.26", "10.0.0.10", protoTCP, 9000, 0, 0, 0), 30),           // opened and closed during the dump
	}, subscribed, e.add)
	got := e.take()
	if len(got) != 1 || got[0].DstPort != 6379 || got[0].Connections != 1 || got[0].Bytes != 0 || !got[0].At.Equal(subscribed) {
		t.Fatalf("dump counted %+v, want only the unseen live connection, first seen at the subscribe, without bytes", got)
	}

	closes := []struct {
		name  string
		flow  *ct.Flow
		conns uint64
	}{
		{"dumped connection", withID(flow("10.0.0.23", "10.0.0.10", protoTCP, 6379, 0, 700, 300), 3), 0},
		{"connection sighted by NEW", withID(flow("10.0.0.20", "10.0.0.10", protoTCP, 5432, 0, 1, 1), 1), 0},
		{"would-block connection", withID(flow("10.0.0.22", "10.0.0.10", protoTCP, 80, wouldBlockMark, 4, 0), 5), 0},
		{"connection never sighted", withID(flow("10.0.0.24", "10.0.0.10", protoUDP, 53, 0, 60, 40), 8), 1},
		{"a later connection reusing a closed id", withID(flow("10.0.0.23", "10.0.0.10", protoTCP, 6379, 0, 1, 0), 3), 1},
	}
	for _, c := range closes {
		o, ok := s.observe(tr, ct.Event{Type: ct.EventDestroy, Flow: c.flow}, now, subscribed)
		if !ok || o.Connections != c.conns || o.Bytes != c.flow.CountersOrig.Bytes+c.flow.CountersReply.Bytes {
			t.Fatalf("%s: close = %+v, %v; want %d connections", c.name, o, ok, c.conns)
		}
	}
	if len(s.Gaps.Pending()) != 0 {
		t.Fatal("a complete dump recorded a gap")
	}

	// The other order: the dump is read before a NEW already queued for
	// a connection that opened between the subscribe and the dump.
	tr = newSightings(100)
	s.fold(tr, []ct.Flow{*withID(flow("10.0.0.25", "10.0.0.10", protoTCP, 8080, 0, 0, 0), 21)}, subscribed, e.add)
	if got := e.take(); len(got) != 1 || got[0].Connections != 1 {
		t.Fatalf("dump read first counted %+v", got)
	}
	if o, ok := s.observe(tr, ct.Event{Type: ct.EventNew, Flow: withID(flow("10.0.0.25", "10.0.0.10", protoTCP, 8080, 0, 0, 0), 21)}, now, subscribed); ok {
		t.Fatalf("NEW after the dump counted the connection again: %+v", o)
	}
}

// TestDumpPastTheCapIsAGap checks the dump's bound: entries past the cap
// are skipped and recorded as a dump_truncated gap with the count
// skipped, a skipped connection closing later is counted at its close
// and widens the gap to that instant, and once the subscription tracks as
// many connections as the cap it counts further connections without
// tracking them.
func TestDumpPastTheCapIsAGap(t *testing.T) {
	s := localSource()
	s.Gaps = &collect.Gaps{}
	subscribed := time.Now()
	tr := newSightings(2)
	var e emitted
	s.fold(tr, []ct.Flow{
		*withID(flow("10.0.0.20", "10.0.0.10", protoTCP, 22, 0, 0, 0), 10),
		*withID(flow("10.0.0.21", "10.0.0.10", protoTCP, 22, 0, 0, 0), 11),
		*withID(flow("10.0.0.22", "10.0.0.10", protoTCP, 22, 0, 0, 0), 12),
		*withID(flow("10.0.0.23", "10.0.0.10", protoTCP, 22, 0, 0, 0), 13),
	}, subscribed, e.add)
	if got := e.take(); len(got) != 2 {
		t.Fatalf("dump counted %d connections past a cap of 2", len(got))
	}
	gaps := s.Gaps.Pending()
	if len(gaps) != 1 || gaps[0].Kind != collect.GapDumpTruncated || gaps[0].Source != collect.GapConntrack || !gaps[0].HasCount || gaps[0].Count != 2 || !gaps[0].From.Equal(subscribed) {
		t.Fatalf("gaps = %+v", gaps)
	}

	later := gaps[0].To.Add(time.Minute)
	o, ok := s.observe(tr, ct.Event{Type: ct.EventDestroy, Flow: withID(flow("10.0.0.22", "10.0.0.10", protoTCP, 22, 0, 5, 5), 12)}, later, subscribed)
	if !ok || o.Connections != 1 {
		t.Fatalf("skipped connection's close = %+v, %v", o, ok)
	}
	gaps = s.Gaps.Pending()
	if len(gaps) != 1 || !gaps[0].To.Equal(later) || gaps[0].Count != 2 {
		t.Fatalf("gaps after a skipped connection closed = %+v", gaps)
	}

	o, ok = s.observe(tr, ct.Event{Type: ct.EventNew, Flow: withID(flow("10.0.0.24", "10.0.0.10", protoTCP, 22, 0, 0, 0), 14)}, later, subscribed)
	if !ok || o.Connections != 1 || !tr.full || len(tr.seen) != 2 {
		t.Fatalf("new past the tracking cap = %+v, %v; full %v, tracked %d", o, ok, tr.full, len(tr.seen))
	}
}

// fakeKernel stands in for the netlink connections: each Listen is a
// subscription the test drives, and each Dump returns the table.
type fakeKernel struct {
	mu       sync.Mutex
	table    []ct.Flow
	dials    int
	dumps    int
	buffers  []int
	listened chan *fakeSub
}

type fakeSub struct {
	events chan<- ct.Event
	errs   chan error
}

type fakeConn struct{ k *fakeKernel }

func (k *fakeKernel) dial() (conn, error) {
	k.mu.Lock()
	k.dials++
	k.mu.Unlock()
	return fakeConn{k}, nil
}

func (c fakeConn) SetReadBuffer(n int) error {
	c.k.mu.Lock()
	c.k.buffers = append(c.k.buffers, n)
	c.k.mu.Unlock()
	return nil
}

func (c fakeConn) Listen(ev chan<- ct.Event, _ uint8, _ []netfilter.NetlinkGroup) (chan error, error) {
	sub := &fakeSub{events: ev, errs: make(chan error, 1)}
	c.k.listened <- sub
	return sub.errs, nil
}

func (c fakeConn) Dump(*ct.DumpOptions) ([]ct.Flow, error) {
	c.k.mu.Lock()
	defer c.k.mu.Unlock()
	c.k.dumps++
	return append([]ct.Flow(nil), c.k.table...), nil
}

func (c fakeConn) Close() error { return nil }

func waitObservation(t *testing.T, e *emitted, what string, match func(collect.Observation) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, o := range e.take() {
			if match(o) {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("no observation: %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestOverrunResubscribesAtOnce checks the recovery path: the kernel's
// ENOBUFS, however wrapped, ends the subscription without ending Run; the
// source subscribes again at once (no backoff: Run never returns), sizes
// the new socket, dumps the table again, counts the overrun, and records
// the interval from the last event read to the new subscribe as an
// overrun gap.
func TestOverrunResubscribesAtOnce(t *testing.T) {
	k := &fakeKernel{listened: make(chan *fakeSub, 4), table: []ct.Flow{
		*withID(flow("10.0.0.20", "10.0.0.10", protoTCP, 5432, 0, 0, 0), 1),
	}}
	s := localSource()
	s.LocalAddresses = func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("10.0.0.10")} }
	s.Gaps = &collect.Gaps{}
	s.ReadBuffer = 1 << 20
	s.dial = k.dial
	var e emitted
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- s.Run(ctx, e.add) }()

	sub := <-k.listened
	dumped := func(o collect.Observation) bool { return o.DstPort == 5432 && o.Connections == 1 }
	waitObservation(t, &e, "the live connection from the first dump", dumped)
	sub.events <- ct.Event{Type: ct.EventNew, Flow: withID(flow("10.0.0.21", "10.0.0.10", protoTCP, 6379, 0, 0, 0), 2)}
	waitObservation(t, &e, "a NEW on the first subscription", func(o collect.Observation) bool { return o.DstPort == 6379 })
	beforeOverrun := time.Now()

	sub.errs <- fmt.Errorf("Receive() netlink error, closing worker 0: %w", os.NewSyscallError("recvmsg", unix.ENOBUFS))
	select {
	case sub = <-k.listened:
	case err := <-returned:
		t.Fatalf("Run returned on an overrun: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no resubscribe after the overrun")
	}
	waitObservation(t, &e, "the live connection from the second dump", dumped)

	if got := s.Gaps.Overruns(); got != 1 {
		t.Fatalf("overruns = %d, want 1", got)
	}
	gaps := s.Gaps.Pending()
	if len(gaps) != 1 || gaps[0].Kind != collect.GapSourceOverrun || gaps[0].Source != collect.GapConntrack || gaps[0].HasCount || gaps[0].From.After(beforeOverrun) || !gaps[0].To.After(gaps[0].From) {
		t.Fatalf("gaps = %+v", gaps)
	}
	k.mu.Lock()
	if k.dumps != 2 || len(k.buffers) != 2 || k.buffers[0] != 1<<20 || k.buffers[1] != 1<<20 {
		t.Fatalf("dumps %d, buffers %v; want a dump and a sized socket per subscribe", k.dumps, k.buffers)
	}
	k.mu.Unlock()

	// Any other stream error is a failure Run returns, for the collector
	// to restart with backoff.
	sub.errs <- errors.New("Receive() netlink error, closing worker 0: bad message")
	select {
	case err := <-returned:
		if err == nil {
			t.Fatal("Run returned nil on a stream failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return on a stream failure")
	}
}
