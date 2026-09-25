package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestAckAndApplyFailureInstants covers the two instants the sync path
// stamps when it records an acknowledgement: an applied version stamps
// the last ack, a failed apply stamps the last failure with the degraded
// state and the detail, and neither is written by anything else. Both
// read back through the registry and both read-model paths.
func TestAckAndApplyFailureInstants(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := enrollWorkload(t, s)
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	lookup := func() *registry.Workload {
		t.Helper()
		w, err := s.LookupWorkload(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	if w := lookup(); w.LastAckedAt != nil || w.LastApplyFailedAt != nil {
		t.Fatalf("fresh workload instants = %v / %v", w.LastAckedAt, w.LastApplyFailedAt)
	}

	// Neither the version a Hello claims nor a plain state change is an
	// acknowledgement.
	if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "0.3.0"}, 1, at); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSyncState(ctx, id, innerwallv1.SyncState_SYNC_STATE_PENDING, "", at); err != nil {
		t.Fatal(err)
	}
	if w := lookup(); w.LastAckedAt != nil || w.LastApplyFailedAt != nil {
		t.Fatalf("instants after Hello and state = %v / %v", w.LastAckedAt, w.LastApplyFailedAt)
	}

	// An applied version stamps the ack.
	acked := at.Add(time.Minute)
	if err := s.RecordApplied(ctx, id, 2, innerwallv1.SyncState_SYNC_STATE_SYNCED, acked); err != nil {
		t.Fatal(err)
	}
	if w := lookup(); w.LastAckedAt == nil || !w.LastAckedAt.Equal(acked) || w.LastApplyFailedAt != nil {
		t.Fatalf("after ack = %v / %v", w.LastAckedAt, w.LastApplyFailedAt)
	}

	// A failed apply stamps the failure with the degraded state and the
	// detail, and leaves the applied version and the ack alone.
	failed := at.Add(2 * time.Minute)
	if err := s.RecordApplyFailed(ctx, id, "apply refused", failed); err != nil {
		t.Fatal(err)
	}
	w := lookup()
	if w.LastApplyFailedAt == nil || !w.LastApplyFailedAt.Equal(failed) || !w.LastAckedAt.Equal(acked) {
		t.Fatalf("after failure = %v / %v", w.LastAckedAt, w.LastApplyFailedAt)
	}
	if w.SyncState != innerwallv1.SyncState_SYNC_STATE_DEGRADED || w.SyncError != "apply refused" || w.AppliedVersion != 2 || !w.LastSeenAt.Equal(failed) {
		t.Fatalf("failure record = %v %q v%d seen %v", w.SyncState, w.SyncError, w.AppliedVersion, w.LastSeenAt)
	}

	// Recovery clears the detail, advances the ack, and keeps the
	// failure instant: the state says the failure no longer stands.
	recovered := at.Add(3 * time.Minute)
	if err := s.RecordApplied(ctx, id, 3, innerwallv1.SyncState_SYNC_STATE_SYNCED, recovered); err != nil {
		t.Fatal(err)
	}
	w = lookup()
	if !w.LastAckedAt.Equal(recovered) || !w.LastApplyFailedAt.Equal(failed) || w.SyncError != "" || w.SyncState != innerwallv1.SyncState_SYNC_STATE_SYNCED {
		t.Fatalf("after recovery = %v / %v %q %v", w.LastAckedAt, w.LastApplyFailedAt, w.SyncError, w.SyncState)
	}

	rec, err := s.GetWorkloadRecord(ctx, id)
	if err != nil || rec.LastAckedAt == nil || !rec.LastAckedAt.Equal(recovered) || rec.LastApplyFailedAt == nil || !rec.LastApplyFailedAt.Equal(failed) {
		t.Fatalf("detail instants = %+v err = %v", rec, err)
	}
	page, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Limit: 10})
	if err != nil || len(page) != 1 || page[0].LastAckedAt == nil || !page[0].LastAckedAt.Equal(recovered) || page[0].LastApplyFailedAt == nil || !page[0].LastApplyFailedAt.Equal(failed) {
		t.Fatalf("page instants = %+v err = %v", page, err)
	}

	unknown, _ := identity.NewWorkloadID()
	if err := s.RecordApplyFailed(ctx, unknown, "x", at); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("unknown err = %v", err)
	}
}
