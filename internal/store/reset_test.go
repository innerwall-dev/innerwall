package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// resetKeeps names the tables of the migrated schema that storetest.Reset
// leaves alone on purpose, each with its reason. Every other table holds
// data and is emptied.
var resetKeeps = map[string]string{
	"goose_db_version": "it is goose's record of the migrations applied, and emptying it would make the next migrate run every migration again over the existing schema",
}

// TestResetTablesAreTheMigratedSchema holds storetest.Reset's table list
// to the tables the migrations create (issue #227): a migration that adds
// a table and leaves it out of the reset would leave its rows behind for
// the next test, the next seed, and `innerwall dev reset`.
func TestResetTablesAreTheMigratedSchema(t *testing.T) {
	storetest.Open(t) // migrates, resets, and holds the database for the test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, os.Getenv(storetest.EnvDatabaseURL))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	rows, err := conn.Query(ctx, `SELECT table_name::text FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}

	reset := map[string]bool{}
	for _, name := range storetest.ResetTables() {
		if reset[name] {
			t.Errorf("storetest.Reset names %s twice", name)
		}
		reset[name] = true
	}
	inSchema := map[string]bool{}
	for _, name := range migrated {
		inSchema[name] = true
		if _, kept := resetKeeps[name]; kept {
			if reset[name] {
				t.Errorf("storetest.Reset empties %s, which it must keep: %s", name, resetKeeps[name])
			}
			continue
		}
		if !reset[name] {
			t.Errorf("the migrations create table %s and storetest.Reset does not empty it: add it to the TRUNCATE statement in internal/storetest/storetest.go", name)
		}
	}
	for name := range reset {
		if !inSchema[name] {
			t.Errorf("storetest.Reset names %s, which no migration creates: remove it from the TRUNCATE statement in internal/storetest/storetest.go", name)
		}
	}
	for name := range resetKeeps {
		if !inSchema[name] {
			t.Errorf("%s is kept by storetest.Reset but is not in the migrated schema: remove it from resetKeeps", name)
		}
	}
}
