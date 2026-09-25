//go:build dev

package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// runDev is the development-only command set, compiled in with the dev
// build tag and absent from every release build. `dev seed` empties the
// database and loads the recognizable fleet the store tests seed, so the
// console can be reviewed against a running control plane with the
// screen states the tests already produce, and without the privileged
// enforcement suite.
func runDev(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "seed" {
		return fmt.Errorf("usage: innerwall dev seed [--database-url URL] [--estate] [--estate-extra N]")
	}
	fs := flag.NewFlagSet("innerwall dev seed", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	estate := fs.Bool("estate", false, "also load the review estate: the flow map's label groups, address groups, and traffic, a few hundred workloads")
	extra := fs.Int("estate-extra", 0, "with --estate, this many more app groups of three workloads each, for reviewing the map at estate scale")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	url, err := databaseURL(*dbURL)
	if err != nil {
		return err
	}
	// The seed is relative to a clock; the fleet's ages (a workload
	// offline for three hours, a credential expired an hour ago) read
	// correctly only against the control plane's own now.
	now := time.Now().UTC().Truncate(time.Second)
	if err := storetest.Reset(ctx, url); err != nil {
		return err
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		return err
	}
	defer st.Close()
	f, err := storetest.Seed(ctx, st, now)
	if err != nil {
		return fmt.Errorf("seeding: %w", err)
	}
	if *estate {
		if err := storetest.SeedEstate(ctx, st, f, *extra); err != nil {
			return fmt.Errorf("seeding the estate: %w", err)
		}
		fmt.Printf("seeded the review estate (%d extra app groups)\n", *extra)
	}
	fmt.Printf("seeded fleet relative to %s: web-1 %s (enforced, synced), db-1 %s (simulation, degraded), cache-1 %s (visibility, offline); ruleset %q; address group %q\n",
		now.Format(time.RFC3339), f.Web, f.DB, f.Cache, f.Ruleset.Name, f.Office.Name)
	fmt.Println("the operator password is not part of the seed; set it with `innerwall operator set-password`")
	return nil
}
