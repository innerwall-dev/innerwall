//go:build !linux

package nflog

import (
	"context"
	"errors"
	"log/slog"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Source is the log event source; unsupported on this host.
type Source struct {
	Group  uint16
	Decide func() innerwallv1.PolicyDecision
	Log    *slog.Logger
}

var _ collect.Source = (*Source)(nil)

// Run implements collect.Source.
func (s *Source) Run(context.Context, func(collect.Observation)) error {
	return errors.New("nflog: log observation is supported on Linux only")
}
