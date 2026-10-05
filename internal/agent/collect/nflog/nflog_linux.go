//go:build linux

package nflog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	nfl "github.com/florianl/go-nflog/v2"
	"golang.org/x/sys/unix"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Source subscribes to one netlink log group and emits an observation per
// logged packet: one connection attempt, with the packet's length as its
// bytes. The decision is asked of Decide at each event, so a mode change
// takes effect on the next packet; because the decision is part of the
// aggregation key, records before and after the change stay apart.
type Source struct {
	// Group is the log group the terminal rule logs to.
	Group uint16
	// Decide returns the decision a logged packet represents under the
	// installed mode: BLOCKED when enforced, WOULD_BLOCK when simulated.
	// A packet whose decision is OBSERVED or unspecified is dropped.
	Decide func() innerwallv1.PolicyDecision
	Log    *slog.Logger
	// Gaps records the evidence this source knows it lost; nil records
	// nothing.
	Gaps *collect.Gaps
	// ReadBuffer is the socket's receive buffer in bytes, set at every
	// subscribe; the kernel's default when zero.
	ReadBuffer int
}

var (
	_ collect.Source = (*Source)(nil)
	_ collect.Gapped = (*Source)(nil)
)

// EvidenceGaps implements collect.Gapped.
func (s *Source) EvidenceGaps() (*collect.Gaps, collect.GapSource) {
	return s.Gaps, collect.GapNflog
}

func (s *Source) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Run implements collect.Source. An overrun is recovered in place: the
// kernel reports it once and the subscription stays bound, so reading
// simply continues, and the interval the kernel dropped packets in is
// recorded as a gap. This matters more here than anywhere: these are the
// would-block and blocked counts, and a lost would-block packet would
// push a simulation's verdict toward safe. Any other failure returns, and
// the collector restarts the source with backoff.
func (s *Source) Run(ctx context.Context, emit func(collect.Observation)) error {
	conn, err := nfl.Open(&nfl.Config{Group: s.Group, Copymode: nfl.CopyPacket, Bufsize: 128, ReadTimeout: time.Second})
	if err != nil {
		return fmt.Errorf("nflog: subscribing to group %d: %w", s.Group, err)
	}
	defer func() { _ = conn.Close() }()
	if s.ReadBuffer > 0 {
		// SO_RCVBUFFORCE first, so a privileged agent is not held to
		// net.core.rmem_max; SO_RCVBUF when that is refused.
		if err := conn.Con.SetReadBuffer(s.ReadBuffer); err != nil {
			s.log().Warn("sizing the log event socket failed; the kernel default stands", "group", s.Group, "bytes", s.ReadBuffer, "error", err)
		}
	}
	// NETLINK_NO_ENOBUFS is deliberately never set. It would silence the
	// kernel's report that packets were dropped while leaving them
	// dropped: the overrun is the only evidence that the counts have a
	// hole in them, and a quiet socket that loses events is worse than a
	// loud one that is recovered and recorded.
	errs := make(chan error, 1)
	// The hook and the error function run on the library's one receive
	// goroutine, in order, so last needs no lock.
	last := time.Now()
	hook := func(a nfl.Attribute) int {
		now := time.Now()
		last = now
		if a.Payload == nil {
			return 0
		}
		if o, ok := s.classify(*a.Payload, now); ok {
			emit(o)
		}
		return 0
	}
	errFn := func(err error) int {
		if ctx.Err() == nil && s.overran(err, last, time.Now()) {
			last = time.Now()
			return 0
		}
		select {
		case errs <- err:
		default:
		}
		return 1
	}
	if err := conn.RegisterWithErrorFunc(ctx, hook, errFn); err != nil {
		return fmt.Errorf("nflog: registering on group %d: %w", s.Group, err)
	}
	subscribed := time.Now()
	s.Gaps.Close(collect.GapNflog, subscribed)
	s.log().Info("log event source started", "group", s.Group)
	select {
	case <-ctx.Done():
		return nil
	case err := <-errs:
		return fmt.Errorf("nflog: event stream: %w", err)
	}
}

// overran reports whether err is the kernel's report that the socket
// overflowed, and if so counts it and records the interval from the last
// event read to now as a gap.
func (s *Source) overran(err error, last, now time.Time) bool {
	if !errors.Is(err, unix.ENOBUFS) {
		return false
	}
	s.Gaps.Overrun()
	s.Gaps.Record(collect.Gap{Kind: collect.GapSourceOverrun, Source: collect.GapNflog, From: last, To: now})
	s.log().Warn("log event socket overran; packets were dropped, reading on", "group", s.Group, "last_event", last)
	return true
}

func (s *Source) classify(payload []byte, now time.Time) (collect.Observation, bool) {
	decision := innerwallv1.PolicyDecision_POLICY_DECISION_UNSPECIFIED
	if s.Decide != nil {
		decision = s.Decide()
	}
	if decision != innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED && decision != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK {
		return collect.Observation{}, false
	}
	p, err := Parse(payload)
	if err != nil {
		return collect.Observation{}, false
	}
	return collect.Observation{At: now, Src: p.Src.Unmap(), Dst: p.Dst.Unmap(), DstPort: p.DstPort, Protocol: p.Protocol, Decision: decision, Connections: 1, Bytes: p.Length}, true
}
