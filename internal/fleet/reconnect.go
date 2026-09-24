package fleet

import (
	"context"
	"errors"
	"fmt"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// Directives sends a directive toward the stream of one workload. The
// store implements it over the notification channel render announcements
// ride, keyed by workload, and the replica holding that workload's stream
// sends the directive (ADR-0018 as amended).
type Directives interface {
	// DirectReconnect asks for a Reconnect directive on the stream of id.
	// It returns once the request is published; whether any replica
	// holds the stream is not known to it.
	DirectReconnect(ctx context.Context, id identity.WorkloadID) error
}

// ErrAgentOffline is returned when a directed reconnect is refused
// because the workload's agent is offline as the read model judges it;
// the error carrying it is an *AgentOfflineError with the last-seen
// instant.
var ErrAgentOffline = errors.New("fleet: the workload's agent is offline; nothing was sent")

// AgentOfflineError is a refused directed reconnect with the instant the
// agent was last heard from, nil when it never was.
type AgentOfflineError struct {
	LastSeenAt *time.Time
}

func (e *AgentOfflineError) Error() string {
	if e.LastSeenAt == nil {
		return ErrAgentOffline.Error() + " (never seen)"
	}
	return fmt.Sprintf("%s (last seen %s)", ErrAgentOffline.Error(), e.LastSeenAt.UTC().Format(time.RFC3339))
}

func (e *AgentOfflineError) Unwrap() error { return ErrAgentOffline }

// ReconnectRequest acknowledges a directed reconnect. LastSnapshotSentAt
// is the workload's snapshot instant as it stood when the directive was
// fired, nil when none has been recorded: the caller observes the outcome
// as a later read showing it advanced.
type ReconnectRequest struct {
	LastSnapshotSentAt *time.Time
}

// RequestReconnect directs the agent of one workload to reconnect, so that
// its stream restarts from a fresh snapshot. It refuses an unknown
// workload with registry.ErrWorkloadUnknown, and refuses with an
// *AgentOfflineError when the read model's sync state says the agent is
// offline. That judgment is the one the fleet screens show, recorded when
// a stream closes; it approximates whether a stream exists and is not a
// presence check, which nothing can make (ADR-0018 as amended).
// Otherwise it fires the directive over the notification bridge and
// returns without waiting: delivery is best-effort by design, and whether
// it worked is read later as the snapshot instant advancing.
func (s *Service) RequestReconnect(ctx context.Context, id identity.WorkloadID) (*ReconnectRequest, error) {
	w, err := s.Reads.GetWorkload(ctx, id)
	if err != nil {
		return nil, err
	}
	if w.Sync.State == innerwallv1.SyncState_SYNC_STATE_OFFLINE {
		return nil, &AgentOfflineError{LastSeenAt: w.Health.LastSeenAt}
	}
	if err := s.Directives.DirectReconnect(ctx, id); err != nil {
		return nil, err
	}
	return &ReconnectRequest{LastSnapshotSentAt: w.Sync.LastSnapshotSentAt}, nil
}
