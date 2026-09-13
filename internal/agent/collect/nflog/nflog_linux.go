//go:build linux

package nflog

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	nfl "github.com/florianl/go-nflog/v2"

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
	conn, err := nfl.Open(&nfl.Config{Group: s.Group, Copymode: nfl.CopyPacket, Bufsize: 128, ReadTimeout: time.Second})
	if err != nil {
		return fmt.Errorf("nflog: subscribing to group %d: %w", s.Group, err)
	}
	defer func() { _ = conn.Close() }()
	errs := make(chan error, 1)
	hook := func(a nfl.Attribute) int {
		if a.Payload == nil {
			return 0
		}
		if o, ok := s.classify(*a.Payload, time.Now()); ok {
			emit(o)
		}
		return 0
	}
	errFn := func(err error) int {
		select {
		case errs <- err:
		default:
		}
		return 1
	}
	if err := conn.RegisterWithErrorFunc(ctx, hook, errFn); err != nil {
		return fmt.Errorf("nflog: registering on group %d: %w", s.Group, err)
	}
	s.log().Info("log event source started", "group", s.Group)
	select {
	case <-ctx.Done():
		return nil
	case err := <-errs:
		return fmt.Errorf("nflog: event stream: %w", err)
	}
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
