package enforce

import (
	"context"
	"errors"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// TestPresenceReportsRemovalOnceAndItsInterval drives the owned table
// through the kill switch and back: nothing is said before a policy is
// installed; a check that finds the table reports it present; the first
// check that finds it gone reports the removal once, from the last instant
// it was known present; later checks repeat the state without reporting
// the removal again; a failed read says nothing either way; and the next
// apply ends the removal at its own instant.
func TestPresenceReportsRemovalOnceAndItsInterval(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	now := t0
	present := true
	var probeErr error
	installed := false
	var removed, restored []time.Time
	p := &Presence{
		Probe:     func(context.Context) (bool, error) { return present, probeErr },
		Installed: func() bool { return installed },
		Removed:   func(since time.Time) { removed = append(removed, since) },
		Restored:  func(at time.Time) { restored = append(restored, at) },
		Now:       func() time.Time { return now },
	}
	store := Watched{PolicyStore: &MemoryStore{}, Presence: p}

	if st := p.Check(ctx); st != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED {
		t.Fatalf("before any install: %v", st)
	}
	installed = true
	if err := store.Apply(ctx, &innerwallv1.WorkloadPolicy{Version: 1}); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(30 * time.Second)
	if st := p.Check(ctx); st != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT {
		t.Fatalf("installed: %v", st)
	}

	// The kill switch runs between two heartbeats.
	present = false
	now = t0.Add(60 * time.Second)
	if st := p.Check(ctx); st != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT {
		t.Fatalf("after the kill switch: %v", st)
	}
	now = t0.Add(90 * time.Second)
	if st := p.Check(ctx); st != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT {
		t.Fatalf("still gone: %v", st)
	}
	if len(removed) != 1 || !removed[0].Equal(t0.Add(30*time.Second)) {
		t.Fatalf("removals = %v, want one from the last check that saw the table", removed)
	}

	probeErr = errors.New("nft: exec failed")
	if st := p.Check(ctx); st != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED {
		t.Fatalf("failed read: %v", st)
	}
	probeErr = nil

	// A resent snapshot reinstalls the table.
	present = true
	now = t0.Add(100 * time.Second)
	if err := store.Apply(ctx, &innerwallv1.WorkloadPolicy{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || !restored[0].Equal(t0.Add(100*time.Second)) {
		t.Fatalf("restorations = %v", restored)
	}
	now = t0.Add(120 * time.Second)
	if st := p.Check(ctx); st != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT || len(restored) != 1 {
		t.Fatalf("after reinstall: %v, restorations %v", st, restored)
	}

	// A refused apply installs nothing and ends nothing.
	present = false
	now = t0.Add(150 * time.Second)
	_ = p.Check(ctx)
	store.PolicyStore = &MemoryStore{Fail: func(*innerwallv1.WorkloadPolicy) error { return errors.New("refused") }}
	if err := store.Apply(ctx, &innerwallv1.WorkloadPolicy{Version: 2}); err == nil {
		t.Fatal("refused apply succeeded")
	}
	if len(removed) != 2 || !removed[1].Equal(t0.Add(120*time.Second)) || len(restored) != 1 {
		t.Fatalf("removals %v restorations %v", removed, restored)
	}
}

// TestPresenceNilSaysNothing keeps the daemon's wiring optional.
func TestPresenceNilSaysNothing(t *testing.T) {
	var p *Presence
	if st := p.Check(context.Background()); st != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED {
		t.Fatalf("nil presence: %v", st)
	}
	p.MarkInstalled(time.Now())
	if err := (Watched{PolicyStore: &MemoryStore{}}).Apply(context.Background(), &innerwallv1.WorkloadPolicy{Version: 1}); err != nil {
		t.Fatal(err)
	}
}
