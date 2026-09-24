package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestSnapshotInstantAndReconnectDirective covers the store's two parts
// of a directed reconnect: the snapshot instant the sync path stamps,
// read back through the registry and both read-model paths, and the
// directive riding the render announcement channel, delivered to the
// directive handler and never mistaken for an announcement.
func TestSnapshotInstantAndReconnectDirective(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := enrollWorkload(t, s)

	w, err := s.LookupWorkload(ctx, id)
	if err != nil || w.LastSnapshotSentAt != nil {
		t.Fatalf("fresh workload instant = %v err = %v", w.LastSnapshotSentAt, err)
	}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	if err := s.RecordSnapshotSent(ctx, id, at); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Minute)
	if err := s.RecordSnapshotSent(ctx, id, later); err != nil {
		t.Fatal(err)
	}
	w, _ = s.LookupWorkload(ctx, id)
	if w.LastSnapshotSentAt == nil || !w.LastSnapshotSentAt.Equal(later) {
		t.Fatalf("registry instant = %v, want %v", w.LastSnapshotSentAt, later)
	}
	rec, err := s.GetWorkloadRecord(ctx, id)
	if err != nil || rec.LastSnapshotSentAt == nil || !rec.LastSnapshotSentAt.Equal(later) {
		t.Fatalf("detail instant = %v err = %v", rec, err)
	}
	page, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Limit: 10})
	if err != nil || len(page) != 1 || page[0].LastSnapshotSentAt == nil || !page[0].LastSnapshotSentAt.Equal(later) {
		t.Fatalf("page instant = %+v err = %v", page, err)
	}
	unknown, _ := identity.NewWorkloadID()
	if err := s.RecordSnapshotSent(ctx, unknown, at); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("unknown err = %v", err)
	}

	announced := make(chan compiler.Announcement, 4)
	directed := make(chan identity.WorkloadID, 4)
	ready := make(chan struct{})
	lctx, lcancel := context.WithCancel(ctx)
	defer lcancel()
	go func() {
		_ = s.ListenPolicyChanges(lctx, nil, func() { close(ready) }, func(c compiler.Announcement) { announced <- c }, func(id identity.WorkloadID) { directed <- id })
	}()
	<-ready
	if err := s.DirectReconnect(ctx, id); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-directed:
		if got != id {
			t.Fatalf("directed %s, want %s", got, id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("directive not delivered")
	}
	select {
	case c := <-announced:
		t.Fatalf("directive delivered as an announcement: %+v", c)
	case <-time.After(200 * time.Millisecond):
	}
	// The directive wrote nothing: the instant is the sync path's alone.
	w, _ = s.LookupWorkload(ctx, id)
	if !w.LastSnapshotSentAt.Equal(later) {
		t.Fatalf("instant moved to %v", w.LastSnapshotSentAt)
	}

	// A listener without a directive handler skips directives and still
	// hears announcements after them.
	lcancel()
	announced2 := make(chan compiler.Announcement, 4)
	ready2 := make(chan struct{})
	l2ctx, l2cancel := context.WithCancel(ctx)
	defer l2cancel()
	go func() {
		_ = s.ListenPolicyChanges(l2ctx, nil, func() { close(ready2) }, func(c compiler.Announcement) { announced2 <- c }, nil)
	}()
	<-ready2
	if err := s.DirectReconnect(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := (&compiler.Engine{Store: s}).Render(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-announced2:
		if c.ID != id || c.Version != 1 {
			t.Fatalf("announcement = %+v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("announcement after a skipped directive not delivered")
	}
}
