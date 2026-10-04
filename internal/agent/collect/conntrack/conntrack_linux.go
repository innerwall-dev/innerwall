//go:build linux

// Package conntrack observes inbound connections through the kernel's
// connection tracking over netlink (ADR-0003). Every subscribe listens for
// NEW and DESTROY events and then dumps the table on a second socket, so a
// connection that predates the subscription is seen as well as one that
// opens after it: a NEW event or a dumped entry counts a connection, and a
// DESTROY event attributes its bytes. Only connections whose original
// destination is one of this host's addresses are inbound and reported
// (ADR-0010); everything else the kernel tracks (forwarded, outbound,
// loopback) is ignored at the source.
//
// A record's connection count is the connections opened, or found live at
// subscription, in its window: a dumped connection is counted once at the
// subscribe that found it, and a DESTROY for a connection the subscription
// never sighted counts it once, found at close. Every connection the
// source observes is therefore counted, and its bytes land on a record
// that has one.
package conntrack

import (
	"context"
	"errors"
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
	"golang.org/x/sys/unix"

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

// conn is the part of a conntrack netlink connection the source uses.
type conn interface {
	SetReadBuffer(bytes int) error
	Listen(evChan chan<- ct.Event, numWorkers uint8, groups []netfilter.NetlinkGroup) (chan error, error)
	Dump(opts *ct.DumpOptions) ([]ct.Flow, error)
	Close() error
}

// errOverrun ends a subscription whose socket overflowed; Run subscribes
// again at once.
var errOverrun = errors.New("conntrack: event socket overrun")

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
	// Gaps records the evidence this source knows it lost; nil records
	// nothing.
	Gaps *collect.Gaps
	// ReadBuffer is the event socket's receive buffer in bytes, set at
	// every subscribe; the kernel's default when zero.
	ReadBuffer int
	// DumpMax caps the table entries processed from the dump at each
	// subscribe, and the connections a subscription tracks to count each
	// once; collect.DefaultBufferRecords when zero.
	DumpMax int

	// dial opens a netlink connection; ct.Dial when nil. Tests replace it.
	dial func() (conn, error)
	// queue replaces eventQueue when positive; tests shrink it.
	queue int
	// afterListen runs between the subscribe and the dump; tests use it.
	afterListen func()

	mu    sync.RWMutex
	local map[netip.Addr]struct{}
}

var (
	_ collect.Source = (*Source)(nil)
	_ collect.Gapped = (*Source)(nil)
)

func (s *Source) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// EvidenceGaps implements collect.Gapped.
func (s *Source) EvidenceGaps() (*collect.Gaps, collect.GapSource) {
	return s.Gaps, collect.GapConntrack
}

func (s *Source) open() (conn, error) {
	if s.dial != nil {
		return s.dial()
	}
	c, err := ct.Dial(nil)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Source) dumpMax() int {
	if s.DumpMax > 0 {
		return s.DumpMax
	}
	return collect.DefaultBufferRecords
}

// Run implements collect.Source. An overrun is recovered here, at once:
// the subscription is replaced and the table dumped again, and the
// interval the kernel dropped events in is recorded as a gap. Any other
// failure returns, and the collector restarts the source with backoff.
func (s *Source) Run(ctx context.Context, emit func(collect.Observation)) error {
	if b, err := os.ReadFile(acctPath); err == nil && strings.TrimSpace(string(b)) != "1" {
		s.log().Warn("connection byte accounting is off; flow byte counts will be zero", "sysctl", "net.netfilter.nf_conntrack_acct")
	}
	s.refreshLocal()
	for {
		err := s.subscribe(ctx, emit)
		if errors.Is(err, errOverrun) {
			continue
		}
		return err
	}
}

type dumpResult struct {
	flows []ct.Flow
	err   error
}

// subscribe runs one subscription: listen, then dump, then events until
// ctx ends or the stream fails. Listening before dumping leaves no
// instant in which a connection could open unseen: one that opens after
// the listen arrives as a NEW event, one that predates it is in the dump,
// and one in both is counted once, whichever is read first.
func (s *Source) subscribe(ctx context.Context, emit func(collect.Observation)) error {
	c, err := s.open()
	if err != nil {
		return fmt.Errorf("conntrack: opening netlink: %w", err)
	}
	queue := eventQueue
	if s.queue > 0 {
		queue = s.queue
	}
	events := make(chan ct.Event, queue)
	var errs chan error
	defer func() { closeListening(c, events, errs) }()
	if s.ReadBuffer > 0 {
		// SO_RCVBUFFORCE first, so a privileged agent is not held to
		// net.core.rmem_max; SO_RCVBUF when that is refused.
		if err := c.SetReadBuffer(s.ReadBuffer); err != nil {
			s.log().Warn("sizing the conntrack event socket failed; the kernel default stands", "bytes", s.ReadBuffer, "error", err)
		}
	}
	// NETLINK_NO_ENOBUFS is deliberately never set. It would silence the
	// kernel's report that events were dropped while leaving them
	// dropped: the overrun is the only evidence that the flow map has a
	// hole in it, and a quiet socket that loses events is worse than a
	// loud one that is recovered and recorded.
	errs, err = c.Listen(events, 1, []netfilter.NetlinkGroup{netfilter.GroupCTNew, netfilter.GroupCTDestroy})
	if err != nil {
		return fmt.Errorf("conntrack: subscribing to events: %w", err)
	}
	subscribed := time.Now()
	s.Gaps.Close(collect.GapConntrack, subscribed)
	s.log().Info("conntrack event source subscribed")
	if s.afterListen != nil {
		s.afterListen()
	}

	dumped := make(chan dumpResult, 1)
	go func() {
		flows, err := s.dumpTable()
		dumped <- dumpResult{flows, err}
	}()

	t := newSightings(s.dumpMax())
	last := subscribed
	refresh := time.NewTicker(localRefresh)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			if errors.Is(err, unix.ENOBUFS) {
				// The kernel dropped events because the socket's buffer
				// was full. What was dropped is unknowable: connections
				// opened and closed in the interval, and the bytes of
				// those that closed. The interval runs from the last
				// event this subscription read to the next subscribe.
				s.Gaps.Overrun()
				s.Gaps.Open(collect.GapConntrack, collect.GapSourceOverrun, last)
				s.log().Warn("conntrack event socket overran; events were dropped, subscribing again", "last_event", last)
				return errOverrun
			}
			// The collector opens the restart gap at this failure.
			return fmt.Errorf("conntrack: event stream: %w", err)
		case <-refresh.C:
			s.refreshLocal()
		case ev := <-events:
			now := time.Now()
			last = now
			if o, ok := s.observe(t, ev, now, subscribed); ok {
				emit(o)
			}
		case r := <-dumped:
			dumped = nil
			if r.err != nil {
				// Without the dump the connections that predate this
				// subscription are unseen until they close.
				s.Gaps.Open(collect.GapConntrack, collect.GapSourceRestart, subscribed)
				return fmt.Errorf("conntrack: dumping the table: %w", r.err)
			}
			s.fold(t, r.flows, subscribed, emit)
		}
	}
}

// closeListening closes a listening connection. Closing waits for the
// library's worker, which may be blocked handing over an event or an
// error nobody will read any more, so both are drained until it is done.
func closeListening(c conn, events <-chan ct.Event, errs <-chan error) {
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-events:
			case <-errs:
			case <-done:
				return
			}
		}
	}()
	_ = c.Close()
	close(done)
}

// dumpTable reads the whole table on a connection of its own: the
// listening one is in multicast mode and cannot carry a request.
func (s *Source) dumpTable() ([]ct.Flow, error) {
	c, err := s.open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	return c.Dump(nil)
}

// sightings is what one subscription knows about the connections it has
// counted, keyed by the kernel's flow id, which is unique among live
// entries. It is bounded: past max connections it stops tracking, and a
// connection it could not track may be counted again when it closes.
type sightings struct {
	max  int
	seen map[uint32]struct{}
	// dumping is set until the dump is folded in; closed holds the
	// connections that closed meanwhile, which the dump may still list.
	dumping bool
	closed  map[uint32]struct{}
	// truncated is set when the dump held more entries than were
	// processed: a connection closing unsighted after that is one the
	// dump skipped.
	truncated bool
	full      bool
}

func newSightings(max int) *sightings {
	return &sightings{max: max, seen: map[uint32]struct{}{}, dumping: true, closed: map[uint32]struct{}{}}
}

// sight records a counted connection, reporting whether tracking had to
// stop for the first time.
func (t *sightings) sight(id uint32) (filled bool) {
	if len(t.seen) >= t.max {
		filled = !t.full
		t.full = true
		return filled
	}
	t.seen[id] = struct{}{}
	return false
}

// forget removes a closing connection and reports whether it was counted.
func (t *sightings) forget(id uint32) bool {
	_, ok := t.seen[id]
	delete(t.seen, id)
	return ok
}

func (s *Source) sighted(t *sightings, id uint32) {
	if t.sight(id) {
		s.log().Warn("conntrack is tracking as many connections as it may; a connection opened past this may be counted again when it closes", "max", t.max)
	}
}

// observe turns one event into an observation, or nothing when the event
// is not an inbound connection to this host.
func (s *Source) observe(t *sightings, ev ct.Event, now, subscribed time.Time) (collect.Observation, bool) {
	if ev.Flow == nil {
		return collect.Observation{}, false
	}
	o, ok := s.inbound(ev.Flow, now)
	if !ok {
		return collect.Observation{}, false
	}
	wouldBlock := o.Decision == innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK
	switch ev.Type {
	case ct.EventNew:
		if wouldBlock {
			// Attempts that simulation would have dropped are counted
			// by the log path, one per packet, exactly as enforcement
			// counts drops; conntrack contributes their bytes only.
			return collect.Observation{}, false
		}
		if _, ok := t.seen[ev.Flow.ID]; ok {
			// The dump was read first and counted it: a connection that
			// opened between the subscribe and the dump is in both.
			return collect.Observation{}, false
		}
		o.Connections = 1
		s.sighted(t, ev.Flow.ID)
	case ct.EventDestroy:
		o.Bytes = ev.Flow.CountersOrig.Bytes + ev.Flow.CountersReply.Bytes
		if t.dumping {
			// The dump in flight may still list it; it must not count
			// a connection that has closed, sighted or not.
			t.closed[ev.Flow.ID] = struct{}{}
		}
		if wouldBlock || t.forget(ev.Flow.ID) {
			// Counted already, here or by the log path: the close adds
			// its bytes to the record, and a window holding only the
			// close holds no new connection.
			return o, true
		}
		// Never sighted: found at close, and counted now, so no
		// connection's bytes land on a record without one.
		o.Connections = 1
		if !t.dumping && t.truncated {
			// The dump skipped it: the truncation's interval reaches at
			// least this far.
			s.Gaps.Record(collect.Gap{Kind: collect.GapDumpTruncated, Source: collect.GapConntrack, From: subscribed, To: now, HasCount: true})
		}
	case ct.EventUnknown, ct.EventUpdate, ct.EventExpNew, ct.EventExpDestroy:
		// Updates are not subscribed to and expectations are not flows.
		return collect.Observation{}, false
	default:
		return collect.Observation{}, false
	}
	return o, true
}

// fold counts the dumped connections the subscription has not already
// sighted, through the same filter and classification as a NEW event,
// with the subscription instant as when each was first seen. The cap
// bounds the entries processed, not the dump's peak memory: the library
// returns the whole table at once, and reading it in bounded batches
// needs a dump loop of the source's own on the netlink connection, the
// recorded path if a table too large to hold once per subscribe is ever
// met. Entries past the cap are skipped and recorded as a gap.
func (s *Source) fold(t *sightings, flows []ct.Flow, subscribed time.Time, emit func(collect.Observation)) {
	skipped := 0
	if len(flows) > t.max {
		skipped = len(flows) - t.max
		flows = flows[:t.max]
	}
	counted := 0
	for i := range flows {
		f := &flows[i]
		o, ok := s.inbound(f, subscribed)
		if !ok {
			continue
		}
		if _, ok := t.seen[f.ID]; ok {
			continue
		}
		if _, ok := t.closed[f.ID]; ok {
			continue
		}
		if o.Decision == innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
			// As for a NEW event: the log path counted it when the
			// terminal rule marked it, which is the only way a
			// connection carries the would-block mark.
			continue
		}
		o.Connections = 1
		s.sighted(t, f.ID)
		emit(o)
		counted++
	}
	t.dumping = false
	t.closed = nil
	if skipped > 0 {
		t.truncated = true
		s.Gaps.Record(collect.Gap{Kind: collect.GapDumpTruncated, Source: collect.GapConntrack, From: subscribed, To: time.Now(), Count: uint64(skipped), HasCount: true})
	}
	s.log().Debug("conntrack table dumped", "entries", len(flows)+skipped, "counted", counted, "skipped", skipped)
}

// inbound maps a flow to an observation without counters, or nothing
// when it is not an inbound connection to this host.
func (s *Source) inbound(f *ct.Flow, now time.Time) (collect.Observation, bool) {
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
