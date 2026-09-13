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
// database. The database is migrated and every table emptied before each
// test that opens it, so it must be disposable.
const EnvDatabaseURL = "INNERWALL_TEST_DATABASE_URL"

// testLockKey is the session-level advisory lock every database test holds
// for its duration. Test packages run in parallel processes against the one
// configured database; the lock serializes them so one package's reset
// never lands in the middle of another package's test.
const testLockKey = 0x1_4e4e_4552_5445

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
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	// Test fixture, not a production query: resets every table so tests
	// start from nothing.
	if _, err := conn.Exec(ctx, "TRUNCATE mode_change_workloads, mode_changes, operator_tokens, operator_sessions, operators, flow_totals, flow_windows, workload_listening_services, workload_addresses, workload_policies, rule_service_entries, rule_service_refs, rule_peer_matches, rule_peers, rules, ruleset_scope_matches, rulesets, address_group_cidrs, address_groups, service_entries, services, workload_labels, workloads, provisioning_token_labels, provisioning_tokens"); err != nil {
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
