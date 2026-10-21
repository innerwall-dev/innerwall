package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// presentedCredentialMigration is the migration that added the stamp.
const presentedCredentialMigration = 18

// migrateTo moves the test database to version with the embedded set:
// a fixture for reading a row written before a migration existed.
func migrateTo(ctx context.Context, t *testing.T, version int64) {
	t.Helper()
	cfg, err := pgx.ParseConfig(os.Getenv(storetest.EnvDatabaseURL))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB := stdlib.OpenDB(*cfg)
	defer func() { _ = sqlDB.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, store.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, version); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, version); err != nil {
		t.Fatal(err)
	}
}

// TestPresentedCredential covers the stamp of the credential a sync
// stream opened on (issue #112): a workload written before the migration
// reads none, through the registry and both read-model paths; a Hello
// that knows of none leaves it so; a Hello with one stamps both halves;
// a renewal moves the issued credential and leaves the stamp; and a
// stream ending leaves it too.
func TestPresentedCredential(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	// A workload enrolled before the migration, which then applies.
	migrateTo(ctx, t, presentedCredentialMigration-1)
	id := enrollWorkload(t, s)
	migrateTo(ctx, t, presentedCredentialMigration)

	read := func() (*registry.Workload, *readmodel.WorkloadRecord, readmodel.WorkloadRecord) {
		t.Helper()
		w, err := s.LookupWorkload(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		rec, err := s.GetWorkloadRecord(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		page, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Limit: 10})
		if err != nil || len(page) != 1 {
			t.Fatalf("page = %+v, %v", page, err)
		}
		return w, rec, page[0]
	}
	wantPresented := func(what string, want *registry.PresentedCredential) {
		t.Helper()
		w, rec, row := read()
		for path, got := range map[string]*registry.PresentedCredential{"registry": w.PresentedCredential, "detail": rec.PresentedCredential, "page": row.PresentedCredential} {
			switch {
			case want == nil && got != nil:
				t.Fatalf("%s: %s presented = %+v, want none", what, path, got)
			case want != nil && (got == nil || got.Serial != want.Serial || !got.ExpiresAt.Equal(want.ExpiresAt)):
				t.Fatalf("%s: %s presented = %+v, want %+v", what, path, got, want)
			}
		}
	}
	wantPresented("a row from before the migration", nil)
	if w, _, _ := read(); w.CredentialSerial != "1" {
		t.Fatalf("issued serial = %q", w.CredentialSerial)
	}

	// A Hello that knows of no credential leaves the stamp absent.
	if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "0.3.0"}, 0, nil, at); err != nil {
		t.Fatal(err)
	}
	wantPresented("a Hello without a credential", nil)

	// A Hello on a credential stamps it.
	first := &registry.PresentedCredential{Serial: "1", ExpiresAt: at.Add(time.Hour)}
	if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "0.3.0"}, 0, first, at); err != nil {
		t.Fatal(err)
	}
	wantPresented("a Hello on the enrolled credential", first)

	// A renewal replaces the issued credential and leaves what the live
	// stream presented: the two now differ.
	renewed := at.Add(10 * time.Minute)
	if err := s.RecordRenewal(ctx, id, "2", renewed.Add(time.Hour), renewed, nil); err != nil {
		t.Fatal(err)
	}
	wantPresented("after a renewal", first)
	if w, _, _ := read(); w.CredentialSerial != "2" || !w.CredentialExpiresAt.Equal(renewed.Add(time.Hour)) {
		t.Fatalf("issued after renewal = %q %v", w.CredentialSerial, w.CredentialExpiresAt)
	}

	// The stream ending leaves it; a Hello without one still does.
	if err := s.SetSyncState(ctx, id, innerwallv1.SyncState_SYNC_STATE_OFFLINE, "", renewed); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "0.3.0"}, 0, nil, renewed); err != nil {
		t.Fatal(err)
	}
	wantPresented("after the stream ended", first)

	// The next stream, on the renewed credential, moves it.
	second := &registry.PresentedCredential{Serial: "2", ExpiresAt: renewed.Add(time.Hour)}
	if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "0.3.0"}, 0, second, renewed.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	wantPresented("a Hello on the renewed credential", second)
}
