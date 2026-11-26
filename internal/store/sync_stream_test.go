package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// syncStreamMigration is the migration that added the stream's name.
const syncStreamMigration = 19

// TestOfflineMarkNamesItsStream covers the conditional offline mark
// (issue #109). A workload written before the migration names no stream,
// and an ending stream's mark is written exactly as before. A stream
// open records PENDING and names the stream; once a newer stream has
// opened, the older stream's mark writes nothing and reports so, and the
// newer stream's state stands; the newer stream's own mark is written.
// An unknown workload is refused by both.
func TestOfflineMarkNamesItsStream(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	migrateTo(ctx, t, syncStreamMigration-1)
	id := enrollWorkload(t, s)
	migrateLatest(ctx, t)

	read := func() *registry.Workload {
		t.Helper()
		w, err := s.LookupWorkload(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	want := func(what string, state innerwallv1.SyncState, seen time.Time) {
		t.Helper()
		w := read()
		if w.SyncState != state || w.LastSeenAt == nil || !w.LastSeenAt.Equal(seen) {
			t.Fatalf("%s: state %v last seen %v, want %v at %v", what, w.SyncState, w.LastSeenAt, state, seen)
		}
	}
	mark := func(stream uuid.UUID, now time.Time) bool {
		t.Helper()
		written, err := s.MarkStreamOffline(ctx, id, stream, now)
		if err != nil {
			t.Fatal(err)
		}
		return written
	}

	// A record from before the migration names no stream: the mark of a
	// stream that was open across the upgrade is written as it was.
	if err := s.RecordApplied(ctx, id, 1, innerwallv1.SyncState_SYNC_STATE_SYNCED, at); err != nil {
		t.Fatal(err)
	}
	if !mark(uuid.New(), at.Add(time.Second)) {
		t.Fatal("the mark against a record naming no stream was not written")
	}
	want("record naming no stream", innerwallv1.SyncState_SYNC_STATE_OFFLINE, at.Add(time.Second))

	// Stream a opens on one replica.
	a := uuid.New()
	if err := s.RecordStreamOpen(ctx, id, a, at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	want("stream a open", innerwallv1.SyncState_SYNC_STATE_PENDING, at.Add(2*time.Second))

	// Stream b opens on another and reaches SYNCED before a's mark lands.
	b := uuid.New()
	if err := s.RecordStreamOpen(ctx, id, b, at.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplied(ctx, id, 1, innerwallv1.SyncState_SYNC_STATE_SYNCED, at.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if mark(a, at.Add(5*time.Second)) {
		t.Fatal("the mark of a stream older than the one the record names was written")
	}
	want("after the stale mark", innerwallv1.SyncState_SYNC_STATE_SYNCED, at.Add(4*time.Second))

	// The newer stream's own mark is written.
	if !mark(b, at.Add(6*time.Second)) {
		t.Fatal("the mark of the stream the record names was not written")
	}
	want("after the current mark", innerwallv1.SyncState_SYNC_STATE_OFFLINE, at.Add(6*time.Second))

	// An unknown workload is refused, never reported as a stale mark.
	unknown, err := identity.NewWorkloadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkStreamOffline(ctx, unknown, b, at); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("mark for an unknown workload = %v, want ErrWorkloadUnknown", err)
	}
	if err := s.RecordStreamOpen(ctx, unknown, b, at); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("stream open for an unknown workload = %v, want ErrWorkloadUnknown", err)
	}
}
