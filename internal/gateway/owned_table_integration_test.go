package gateway_test

import (
	"context"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestKillSwitchIsReportedOnTheSyncPath walks issue #64 over a real
// stream: an enforced workload synced on its version reports its owned
// table present; after the kill switch its next heartbeat says absent and
// the record says so, with the instant, while the sync state still names
// the acknowledged version; a heartbeat whose read failed changes nothing;
// and the applied acknowledgement of a resent snapshot, which reinstalls
// the table, records it present again.
func TestKillSwitchIsReportedOnTheSyncPath(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")
	a.connect(0, nil)
	a.expectHelloAck()
	v := a.expectSnapshot().GetVersion()
	a.ack(v, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	heartbeat := func(state innerwallv1.OwnedTableState) {
		a.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Heartbeat{Heartbeat: &innerwallv1.Heartbeat{UptimeSeconds: 30, OwnedTable: state}}})
	}
	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT)
	waitFor(t, st, a.id, "present", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.OwnedTable == innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT
	})

	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT)
	w := waitFor(t, st, a.id, "absent", func(w *registry.Workload) bool {
		return w.OwnedTable == innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT
	})
	if w.OwnedTableAbsentSince == nil || w.SyncState != innerwallv1.SyncState_SYNC_STATE_SYNCED || w.AppliedVersion != v {
		t.Fatalf("after the kill switch: since %v, %v v%d", w.OwnedTableAbsentSince, w.SyncState, w.AppliedVersion)
	}
	since := *w.OwnedTableAbsentSince

	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED)
	a.send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Heartbeat{Heartbeat: &innerwallv1.Heartbeat{UptimeSeconds: 60, DroppedFlowRecords: 1, OwnedTable: innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT}}})
	w = waitFor(t, st, a.id, "still absent", func(w *registry.Workload) bool { return w.DroppedFlowRecords == 1 })
	if w.OwnedTable != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT || w.OwnedTableAbsentSince == nil || !w.OwnedTableAbsentSince.Equal(since) {
		t.Fatalf("a later heartbeat moved the removal: %v since %v, want since %v", w.OwnedTable, w.OwnedTableAbsentSince, since)
	}

	// The operator resends a snapshot; the agent applies it, which
	// reinstalls the table.
	a.disconnect()
	a.connect(v, nil)
	a.expectHelloAck()
	a.expectSnapshot()
	a.ack(v, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	w = waitFor(t, st, a.id, "present after the resent snapshot", func(w *registry.Workload) bool {
		return w.OwnedTable == innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT
	})
	if w.OwnedTableAbsentSince != nil {
		t.Fatalf("absent instant after reinstall = %v", w.OwnedTableAbsentSince)
	}
}
