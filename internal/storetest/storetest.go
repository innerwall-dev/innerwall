// Package storetest opens the Postgres-backed store for tests. Tests that
// need a database skip unless INNERWALL_TEST_DATABASE_URL is set; CI sets it
// to a service container, and the local loop sets it to whatever Postgres
// the developer has.
package storetest

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/innerwall-dev/innerwall/internal/store"
)

// EnvDatabaseURL names the environment variable that points tests at a
// database. The database is migrated and its enrollment tables emptied
// before each test that opens it, so it must be disposable.
const EnvDatabaseURL = "INNERWALL_TEST_DATABASE_URL"

// Open migrates the test database, empties the enrollment tables, and
// returns an open store. It skips the test when no database is configured.
func Open(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv(EnvDatabaseURL)
	if url == "" {
		t.Skipf("%s not set", EnvDatabaseURL)
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	// Test fixture, not a production query: resets the tables this
	// milestone owns so tests start from nothing.
	if _, err := conn.Exec(ctx, "TRUNCATE workload_labels, workloads, provisioning_token_labels, provisioning_tokens"); err != nil {
		t.Fatalf("resetting test database: %v", err)
	}
	_ = conn.Close(ctx)

	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}
