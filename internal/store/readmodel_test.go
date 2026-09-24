package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestWorkloadPage checks the fleet order, the filters, the cursor walk
// under an insert, and the detail read with its latest version.
func TestWorkloadPage(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)

	page, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 3 || page[0].ID != f.DB || page[1].ID != f.Cache || page[2].ID != f.Web {
		t.Fatalf("fleet order = %v, want degraded db-1, offline cache-1, synced web-1", ids(page))
	}
	if page[0].SyncRank != 0 || page[1].SyncRank != 1 || page[2].SyncRank != 3 {
		t.Fatalf("ranks = %d %d %d", page[0].SyncRank, page[1].SyncRank, page[2].SyncRank)
	}
	if page[2].LatestVersion == 0 || page[2].LatestRenderedAt == nil || page[2].AppliedVersion != page[2].LatestVersion {
		t.Fatalf("web-1 versions = applied %d latest %d rendered %v", page[2].AppliedVersion, page[2].LatestVersion, page[2].LatestRenderedAt)
	}
	if page[0].LatestVersion != f.DBVersion || page[0].AppliedVersion != 0 || len(page[0].ListeningServices) != 1 || page[0].ListeningServices[0].Port != 5432 {
		t.Fatalf("db-1 record = %+v", page[0])
	}
	if len(page[0].Labels) != 2 || len(page[0].Addresses) != 1 || page[0].Addresses[0] != f.DBAddr {
		t.Fatalf("db-1 children = labels %v addresses %v", page[0].Labels, page[0].Addresses)
	}

	// Filters.
	sim, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(sim) != 1 || sim[0].ID != f.DB {
		t.Fatalf("mode filter = %v", ids(sim))
	}
	off, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{SyncState: innerwallv1.SyncState_SYNC_STATE_OFFLINE, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 1 || off[0].ID != f.Cache {
		t.Fatalf("sync state filter = %v", ids(off))
	}
	some, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{IDs: []identity.WorkloadID{f.Web, f.DB}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(some) != 2 || some[0].ID != f.DB || some[1].ID != f.Web {
		t.Fatalf("id filter = %v", ids(some))
	}

	// A cursor walk of one row per page, with a workload enrolled after
	// the first page: every row that existed when the walk began is
	// served exactly once, and the newcomer lands in its place.
	first, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	newcomer, _ := identity.NewWorkloadID()
	if err := s.CreateWorkload(ctx, enroll.Workload{ID: newcomer, TokenID: f.Token.ID, Hostname: "web-2", EnrolledAt: f.Now, CredentialSerial: "web-2-1", CredentialExpiresAt: f.Now.Add(time.Hour)}, f.Now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSyncState(ctx, newcomer, innerwallv1.SyncState_SYNC_STATE_PENDING, "", f.Now); err != nil {
		t.Fatal(err)
	}
	walked := ids(first)
	after := &readmodel.WorkloadCursor{SyncRank: first[0].SyncRank, SeenKey: first[0].SeenKey, ID: first[0].ID}
	for {
		p, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Limit: 1, After: after})
		if err != nil {
			t.Fatal(err)
		}
		if len(p) == 0 {
			break
		}
		walked = append(walked, p[0].ID)
		after = &readmodel.WorkloadCursor{SyncRank: p[0].SyncRank, SeenKey: p[0].SeenKey, ID: p[0].ID}
	}
	want := []identity.WorkloadID{f.DB, f.Cache, newcomer, f.Web}
	if len(walked) != len(want) {
		t.Fatalf("walked %v, want %v", walked, want)
	}
	for i := range want {
		if walked[i] != want[i] {
			t.Fatalf("walked %v, want %v", walked, want)
		}
	}

	// The detail read.
	rec, err := s.GetWorkloadRecord(ctx, f.DB)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Hostname != "db-1" || rec.LatestVersion != f.DBVersion || rec.LatestRenderedAt == nil || rec.CredentialRenewalError == "" || rec.DroppedFlowRecords != 42 || rec.LastRenewedAt != nil {
		t.Fatalf("db-1 detail = %+v", rec)
	}
	if rec.SyncState != innerwallv1.SyncState_SYNC_STATE_DEGRADED || rec.SyncError == "" {
		t.Fatalf("db-1 sync = %v %q", rec.SyncState, rec.SyncError)
	}
	if len(rec.ListeningServices) != 1 || rec.ListeningServices[0].ProcessName != "postgres" {
		t.Fatalf("db-1 listening = %+v", rec.ListeningServices)
	}
	if _, err := s.GetWorkloadRecord(ctx, identity.FromUUID(uuid.New())); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("unknown detail err = %v", err)
	}
	none, err := s.GetWorkloadRecord(ctx, newcomer)
	if err != nil {
		t.Fatal(err)
	}
	if none.LatestVersion != 0 || none.LatestRenderedAt != nil {
		t.Fatalf("unrendered workload = version %d rendered %v", none.LatestVersion, none.LatestRenderedAt)
	}

	index, err := s.ListWorkloadLabelIndex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 4 || index[f.DB]["role"] != "db" || len(index[newcomer]) != 0 {
		t.Fatalf("label index = %v", index)
	}
}

func ids(records []readmodel.WorkloadRecord) []identity.WorkloadID {
	out := make([]identity.WorkloadID, 0, len(records))
	for i := range records {
		out = append(out, records[i].ID)
	}
	return out
}

// TestSeedTokens checks the seed's token listing: the prod token without
// a listing hint, one revoked and one expired with hints, and only prod
// used.
func TestSeedTokens(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)

	tokens, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uuid.UUID]enroll.Token{}
	for _, tok := range tokens {
		byID[tok.ID] = tok
	}
	if len(tokens) != 3 {
		t.Fatalf("tokens = %d, want 3", len(tokens))
	}
	if prod := byID[f.Token.ID]; prod.Prefix != nil || prod.UseCount != 3 {
		t.Fatalf("prod = prefix %v, uses %d", prod.Prefix, prod.UseCount)
	}
	revoked := byID[f.RevokedToken.ID]
	if revoked.RevokedAt == nil || revoked.Prefix == nil || revoked.UseCount != 0 {
		t.Fatalf("revoked = %+v", revoked)
	}
	expired := byID[f.ExpiredToken.ID]
	if !expired.ExpiresAt.Before(f.Now) || expired.RevokedAt != nil || expired.Prefix == nil || expired.LastUsedAt != nil {
		t.Fatalf("expired = %+v", expired)
	}
}
