// Command innerwall is the control plane: one binary containing the API
// service, agent gateway, policy compiler, flow ingestion, and signing
// authority. All durable state lives in Postgres; replicas are stateless and
// interchangeable. The signing authority's key is the one exception:
// operator-provisioned configuration, identical on every replica (ADR-0017).
//
// Subcommands:
//
//	serve          run the agent gateway (enrollment, renewal, the sync stream)
//	               and the operator surface (REST/JSON façade, console)
//	migrate        apply pending database migrations
//	ca init        create the file-backed signing authority and a server certificate
//	token          mint, list, and revoke provisioning tokens
//	operator       set the operator password; mint, list, and revoke
//	               operator tokens (ADR-0021)
//	service        author reusable protocol/port sets
//	address-group  author named CIDR sets
//	ruleset        author rulesets from JSON documents
//	workload       inspect workloads; set labels and enforcement mode
//	policy         force a render; show a workload's rendered policy
//	flows          query stored flow records: a workload's windows, a rollup
//	               over a label scope, a workload's totals since first seen
//	dev            development-only commands, compiled in with the dev build
//	               tag: seed loads the recognizable review fleet into a
//	               disposable database so the console can be reviewed
//
// Authoring commands write Postgres directly and render there; the running
// control plane learns of changed policy through the database and pushes
// it to connected agents (ADR-0018). The operator surface serves the
// session and identity endpoints of the REST surface (ADR-0021), the read
// model behind the console's screens (flow rollups, flow pages, workloads,
// rendered policy), and the write paths (authoring, label edits, bulk mode
// changes, selector preview, dry-run render, token management). Every
// command here and every handler there calls the same domain functions,
// so the two transports cannot drift.
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
	envSite        = "INNERWALL_SITE"

	defaultCADir          = "/var/lib/innerwall/ca"
	defaultListen         = ":8443"
	defaultOperatorListen = ":8080"
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
  serve          run the agent gateway and the operator surface
  migrate        apply pending database migrations
  ca init        create the signing authority and server certificate
  token          mint | list | revoke provisioning tokens
  operator       set-password | token mint | token list | token revoke
  service        create | update | delete | get | list services
  address-group  create | update | delete | get | list address groups
  ruleset        create | update | delete | get | list rulesets (JSON documents)
  workload       list | status | set-labels | set-mode
  policy         render | show
  flows          list | rollup | totals
  dev            seed: load the recognizable review fleet (builds with -tags dev only)
  version        print the version

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
	case "operator":
		return runOperator(ctx, args[1:])
	case "service":
		return runService(ctx, args[1:])
	case "address-group":
		return runAddressGroup(ctx, args[1:])
	case "ruleset":
		return runRuleset(ctx, args[1:])
	case "workload":
		return runWorkload(ctx, args[1:])
	case "policy":
		return runPolicy(ctx, args[1:])
	case "flows":
		return runFlows(ctx, args[1:])
	case "dev":
		return runDev(ctx, args[1:])
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
