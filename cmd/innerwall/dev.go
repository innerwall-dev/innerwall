//go:build dev

package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/innerwall-dev/innerwall/internal/cli"
	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// runDev is the development-only command set, compiled in with the dev
// build tag and absent from every release build. `dev seed` empties the
// database and loads the recognizable fleet the store tests seed, so the
// console can be reviewed against a running control plane with the
// screen states the tests already produce, and without the privileged
// enforcement suite. `dev reset` empties the database and loads nothing,
// for returning to an empty control plane after a seed or test agents.
func runDev(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "seed":
			return runDevSeed(ctx, args[1:])
		case "reset":
			return runDevReset(ctx, args[1:])
		}
	}
	return fmt.Errorf("usage: innerwall dev seed [--database-url URL] [--estate] [--estate-extra N] | innerwall dev reset [--database-url URL]")
}

// runDevReset migrates the database and empties every table, exactly the
// first half of seed. The signing authority is files in the CA directory,
// not rows, so it survives and ca.crt stays valid; an agent enrolled
// before the reset holds a credential for a workload that no longer
// exists, and enrolls again as a new one.
func runDevReset(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall dev reset", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := cli.Parse(fs, args, 0); err != nil {
		return err
	}
	url, err := databaseURL(*dbURL)
	if err != nil {
		return err
	}
	if err := storetest.Reset(ctx, url); err != nil {
		return err
	}
	fmt.Println("reset the database: every table is empty")
	fmt.Println("the operator password is not kept; set it with `innerwall operator set-password`")
	fmt.Println("the signing authority in --ca-dir is untouched; ca.crt handed to agents stays valid")
	fmt.Println("a host enrolled before the reset names a workload that no longer exists: stop its daemon, move its state directory aside, and enroll it with a new provisioning token")
	return nil
}

// runDevSeed resets the database and loads the review fleet, and with
// --estate the review estate on top of it.
func runDevSeed(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall dev seed", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	estate := fs.Bool("estate", false, "also load the review estate: the flow map's label groups, address groups, and traffic, a few hundred workloads, and one retired workload")
	extra := fs.Int("estate-extra", 0, "with --estate, this many more app groups of three workloads each, for reviewing the map at estate scale")
	if err := cli.Parse(fs, args, 0); err != nil {
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
		if _, err := storetest.SeedRetired(ctx, st, f); err != nil {
			return fmt.Errorf("seeding the retired workload: %w", err)
		}
		fmt.Printf("seeded the review estate (%d extra app groups) and the retired workload %s\n", *extra, storetest.RetiredHost)
	}
	fmt.Printf("seeded fleet relative to %s: web-1 %s (enforced, synced), db-1 %s (simulation, degraded), cache-1 %s (visibility, offline); ruleset %q; address group %q\n",
		now.Format(time.RFC3339), f.Web, f.DB, f.Cache, f.Ruleset.Name, f.Office.Name)
	fmt.Println("the operator password is not part of the seed; set it with `innerwall operator set-password`")
	return nil
}
