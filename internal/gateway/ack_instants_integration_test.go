package gateway_test

import (
	"context"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestAckInstantsOnTheSyncPath checks that the sync path stamps the two
// acknowledgement instants where it records the acknowledgement: an
// APPLIED ack stamps the last ack, a FAILED ack stamps the last failure
// beside the degraded state and the agent's detail, and the version a
// Hello claims stamps neither. The recovery ack advances the ack and
// leaves the failure instant where it was.
func TestAckInstantsOnTheSyncPath(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")

	// Hello claims a version; that is not an acknowledgement.
	a.connect(1, nil)
	a.expectHelloAck()
	a.expectSnapshot()
	w := waitFor(t, st, a.id, "pending after snapshot", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_PENDING
	})
	if w.LastAckedAt != nil || w.LastApplyFailedAt != nil {
		t.Fatalf("instants before any ack = %v / %v", w.LastAckedAt, w.LastApplyFailedAt)
	}

	a.ack(1, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	w = waitFor(t, st, a.id, "ack instant", func(w *registry.Workload) bool { return w.LastAckedAt != nil })
	acked := *w.LastAckedAt
	if w.SyncState != innerwallv1.SyncState_SYNC_STATE_SYNCED || w.LastApplyFailedAt != nil {
		t.Fatalf("after APPLIED: %v, failure instant %v", w.SyncState, w.LastApplyFailedAt)
	}

	a.ack(1, innerwallv1.AckStatus_ACK_STATUS_FAILED, "simulated apply failure")
	w = waitFor(t, st, a.id, "failure instant", func(w *registry.Workload) bool { return w.LastApplyFailedAt != nil })
	failed := *w.LastApplyFailedAt
	if w.SyncState != innerwallv1.SyncState_SYNC_STATE_DEGRADED || w.SyncError != "simulated apply failure" || w.AppliedVersion != 1 {
		t.Fatalf("after FAILED: %v %q v%d", w.SyncState, w.SyncError, w.AppliedVersion)
	}
	if !w.LastAckedAt.Equal(acked) || failed.Before(acked) {
		t.Fatalf("FAILED moved the ack to %v, or failed at %v before the ack at %v", w.LastAckedAt, failed, acked)
	}

	// The recovery snapshot, applied: the ack advances and the failure
	// instant stays.
	a.expectSnapshot()
	a.ack(1, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	w = waitFor(t, st, a.id, "ack after recovery", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.LastAckedAt.After(acked)
	})
	if w.LastApplyFailedAt == nil || !w.LastApplyFailedAt.Equal(failed) || w.SyncError != "" {
		t.Fatalf("after recovery: failure instant %v (want %v), detail %q", w.LastApplyFailedAt, failed, w.SyncError)
	}
}
