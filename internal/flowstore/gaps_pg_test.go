package flowstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestGapsStoreReadAndPrune checks evidence gaps in Postgres: they are
// stored with a window or on their own, a repeated interval is stored
// once (delivery is at least once), a count survives only where one was
// reported, the read returns the gaps of a workload set intersecting a
// half-open range (an instantaneous gap when its instant lies in it),
// newest first and bounded, and retention deletes a gap only once it
// ended before the horizon.
func TestGapsStoreReadAndPrune(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)
	flows := s.Flows()
	at := func(d time.Duration) time.Time { return f.Now.Add(d) }
	seven := uint64(7)
	overrun := flowstore.Gap{Kind: innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_OVERRUN, Source: innerwallv1.EvidenceSource_EVIDENCE_SOURCE_NFLOG, From: at(-2 * time.Hour), To: at(-119 * time.Minute)}
	overflow := flowstore.Gap{Kind: innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_BUFFER_OVERFLOW, From: at(-3 * time.Hour), To: at(-170 * time.Minute), Count: &seven}
	instant := flowstore.Gap{Kind: innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_DUMP_TRUNCATED, Source: innerwallv1.EvidenceSource_EVIDENCE_SOURCE_CONNTRACK, From: at(-30 * time.Minute), To: at(-30 * time.Minute), Count: &seven}
	restart := flowstore.Gap{Kind: innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_SOURCE_RESTART, Source: innerwallv1.EvidenceSource_EVIDENCE_SOURCE_CONNTRACK, From: at(-10 * time.Minute), To: at(-5 * time.Minute)}

	gapsOnly := flowstore.Window{WorkloadID: f.DB, Start: at(-time.Minute), End: f.Now, Gaps: []flowstore.Gap{overrun, overflow, instant}}
	for range 2 {
		if n, err := flows.WriteWindow(ctx, gapsOnly); err != nil || n != 0 {
			t.Fatalf("writing gaps alone: %d, %v", n, err)
		}
	}
	if _, err := flows.WriteWindow(ctx, flowstore.Window{WorkloadID: f.Web, Start: at(-time.Minute), End: f.Now, Gaps: []flowstore.Gap{restart}}); err != nil {
		t.Fatal(err)
	}

	all, err := flows.ListGaps(ctx, flowstore.GapQuery{Since: at(-24 * time.Hour), Until: f.Now})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("stored %d gaps, want 4 (a repeat is stored once): %+v", len(all), all)
	}
	if all[0].WorkloadID != f.Web || all[1].Kind != instant.Kind || all[3].Count == nil || *all[3].Count != 7 || all[2].Count != nil {
		t.Fatalf("newest first with counts = %+v", all)
	}

	read := func(ids []identity.WorkloadID, since, until time.Time, limit int) []flowstore.GapRow {
		t.Helper()
		rows, err := flows.ListGaps(ctx, flowstore.GapQuery{WorkloadIDs: ids, Since: since, Until: until, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	db := []identity.WorkloadID{f.DB}
	// The overrun ends where the range starts: a half-open interval
	// does not reach it.
	if rows := read(db, at(-119*time.Minute), f.Now, 0); len(rows) != 1 || rows[0].Kind != instant.Kind {
		t.Fatalf("range starting at the overrun's end = %+v", rows)
	}
	if rows := read(db, at(-2*time.Hour), f.Now, 0); len(rows) != 2 {
		t.Fatalf("range starting at the overrun's start = %+v", rows)
	}
	// An instantaneous gap at the range's end is outside it; at its
	// start, inside.
	if rows := read(db, at(-time.Hour), at(-30*time.Minute), 0); len(rows) != 0 {
		t.Fatalf("instant at the range's end = %+v", rows)
	}
	if rows := read(db, at(-30*time.Minute), f.Now, 0); len(rows) != 1 {
		t.Fatalf("instant at the range's start = %+v", rows)
	}
	// Scoped to a set, and bounded.
	if rows := read([]identity.WorkloadID{f.Web, f.Cache}, at(-24*time.Hour), f.Now, 0); len(rows) != 1 || rows[0].Kind != restart.Kind {
		t.Fatalf("web and cache = %+v", rows)
	}
	if rows := read(nil, at(-24*time.Hour), f.Now, 2); len(rows) != 2 || rows[0].WorkloadID != f.Web {
		t.Fatalf("bounded = %+v", rows)
	}

	// Retention: the overflow ended before the horizon; the overrun,
	// which started before it but ends after, stays.
	if _, ran, err := flows.PruneWindows(ctx, at(-150*time.Minute)); err != nil || !ran {
		t.Fatalf("prune: ran %v, %v", ran, err)
	}
	rows := read(nil, at(-24*time.Hour), f.Now, 0)
	if len(rows) != 3 {
		t.Fatalf("after retention = %+v", rows)
	}
	for _, r := range rows {
		if r.Kind == overflow.Kind {
			t.Fatal("a gap that ended before the horizon survived retention")
		}
	}
}

// TestWindowOverflowGapStored checks that the window-overflow kind the
// agent reports when its open window is full is admitted and read back
// with its count, beside the four kinds before it (migration 00009).
func TestWindowOverflowGapStored(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)
	flows := s.Flows()
	dropped := uint64(1234)
	gap := flowstore.Gap{Kind: innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_WINDOW_OVERFLOW, From: f.Now.Add(-40 * time.Second), To: f.Now, Count: &dropped}
	if _, err := flows.WriteWindow(ctx, flowstore.Window{WorkloadID: f.DB, Start: f.Now.Add(-time.Minute), End: f.Now, Gaps: []flowstore.Gap{gap}}); err != nil {
		t.Fatal(err)
	}
	rows, err := flows.ListGaps(ctx, flowstore.GapQuery{WorkloadIDs: []identity.WorkloadID{f.DB}, Since: f.Now.Add(-time.Hour), Until: f.Now})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Kind != gap.Kind || rows[0].Source != innerwallv1.EvidenceSource_EVIDENCE_SOURCE_UNSPECIFIED || rows[0].Count == nil || *rows[0].Count != dropped {
		t.Fatalf("stored window-overflow gap = %+v", rows)
	}
}
