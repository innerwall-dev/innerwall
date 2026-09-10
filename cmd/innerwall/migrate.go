package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/innerwall-dev/innerwall/internal/store"
)

func runMigrate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall migrate", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	url, err := databaseURL(*dbURL)
	if err != nil {
		return err
	}
	if err := store.Migrate(ctx, url); err != nil {
		return err
	}
	fmt.Println("migrations applied")
	return nil
}
