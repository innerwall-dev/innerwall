// Command innerwall is the control plane: one binary containing the API
// service, agent gateway, policy compiler, flow ingestion, and embedded CA.
// All durable state lives in Postgres; replicas are stateless and
// interchangeable (ADR-0005).
//
// Subcommands:
//
//	serve     run the agent gateway (enrollment and credential renewal)
//	migrate   apply pending database migrations
//	ca init   create the file-backed signing authority and a server certificate
//	token     mint, list, and revoke provisioning tokens
//
// The desired-state stream, flow ingestion, and the REST façade are wired in
// by later milestones.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// version is set at link time by the release build (see .goreleaser.yaml).
var version = "dev"

const (
	envDatabaseURL = "INNERWALL_DATABASE_URL"
	envCADir       = "INNERWALL_CA_DIR"

	defaultCADir  = "/var/lib/innerwall/ca"
	defaultListen = ":8443"
)

func main() {
	os.Exit(exitCode())
}

func exitCode() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	err := run(ctx, os.Args[1:])
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 2
	default:
		fmt.Fprintln(os.Stderr, "innerwall:", err)
		return 1
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `innerwall %s — control plane

usage: innerwall <command> [flags]

commands:
  serve      run the agent gateway
  migrate    apply pending database migrations
  ca init    create the signing authority and server certificate
  token      mint | list | revoke provisioning tokens
  version    print the version

Run "innerwall <command> -h" for the flags of a command.
`, version)
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage()
		return flag.ErrHelp
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:])
	case "migrate":
		return runMigrate(ctx, args[1:])
	case "ca":
		return runCA(args[1:])
	case "token":
		return runToken(ctx, args[1:])
	case "version":
		fmt.Println("innerwall", version)
		return nil
	case "-h", "--help", "help":
		usage()
		return flag.ErrHelp
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// envOr returns the environment value for key, or fallback when unset.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// databaseURL resolves the database flag against the environment.
func databaseURL(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv(envDatabaseURL); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no database configured: set --database-url or %s", envDatabaseURL)
}
