//go:build !linux

// Package conntrack observes inbound connections through the kernel's
// connection tracking events. Only Linux is supported in v1; on any other
// host the source reports that it cannot observe.
package conntrack

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Source is the conntrack event source; unsupported on this host.
type Source struct {
	Log            *slog.Logger
	Classify       func(mark uint32) (innerwallv1.PolicyDecision, string)
	LocalAddresses func() []netip.Addr
}

var _ collect.Source = (*Source)(nil)

// Run implements collect.Source.
func (s *Source) Run(context.Context, func(collect.Observation)) error {
	return errors.New("conntrack: flow observation is supported on Linux only")
}
