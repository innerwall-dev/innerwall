// Package storetest opens the Postgres-backed store for tests. Tests that
// need a database skip unless INNERWALL_TEST_DATABASE_URL is set; CI sets it
// to a service container, and the local loop sets it to whatever Postgres
// the developer has.
package storetest

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/innerwall-dev/innerwall/internal/store"
)

// EnvDatabaseURL names the environment variable that points tests at a
// database. The database is migrated and every table emptied before each
// test that opens it, so it must be disposable.
const EnvDatabaseURL = "INNERWALL_TEST_DATABASE_URL"

// testLockKey is the session-level advisory lock every database test holds
// for its duration. Test packages run in parallel processes against the one
// configured database; the lock serializes them so one package's reset
// never lands in the middle of another package's test.
const testLockKey = 0x1_4e4e_4552_5445

// Reset migrates the database at url and empties every table, so what
// follows starts from nothing. It is a fixture, not a production query,
// and the database must be disposable: the test loop and the development
// seed command are its only callers.
func Reset(ctx context.Context, url string) error {
	if err := store.Migrate(ctx, url); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "TRUNCATE mode_change_workloads, mode_changes, operator_tokens, operator_sessions, operators, flow_totals, flow_windows, workload_listening_services, workload_addresses, workload_policies, rule_service_entries, rule_service_refs, rule_peer_matches, rule_peers, rules, ruleset_scope_matches, rulesets, address_group_cidrs, address_groups, service_entries, services, workload_labels, workloads, provisioning_token_labels, provisioning_tokens"); err != nil {
		return fmt.Errorf("resetting database: %w", err)
	}
	return nil
}

// Open migrates the test database, empties every table, and
// returns an open store. It skips the test when no database is configured.
// The database is held exclusively until the test ends.
func Open(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv(EnvDatabaseURL)
	if url == "" {
		t.Skipf("%s not set", EnvDatabaseURL)
	}
	ctx := context.Background()
	lockConn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock($1)", testLockKey); err != nil {
		t.Fatalf("locking test database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = lockConn.Exec(ctx, "SELECT pg_advisory_unlock($1)", testLockKey)
		_ = lockConn.Close(ctx)
	})
	if err := Reset(ctx, url); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}
