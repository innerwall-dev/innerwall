// Command innerwall-agent runs on each workload. It observes flows and
// programs the host's native firewall from desired state pushed by the control
// plane. It dials out only, never listens, never self-updates, and fails
// static: losing the control plane changes nothing on the host (ADR-0011).
//
// Subcommands:
//
//	enroll   exchange a provisioning token for a workload credential
//	renew    rotate the workload credential over mutual TLS
//
// The daemon (sync, collect, reconcile, health) and its automatic renewal
// timer are wired in by later milestones.
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
		fmt.Fprintln(os.Stderr, "innerwall-agent:", err)
		return 1
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `innerwall-agent %s

usage: innerwall-agent <command> [flags]

commands:
  enroll     exchange a provisioning token for a workload credential
  renew      rotate the workload credential
  version    print the version

Run "innerwall-agent <command> -h" for the flags of a command.
`, version)
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage()
		return flag.ErrHelp
	}
	switch args[0] {
	case "enroll":
		return runEnroll(ctx, args[1:])
	case "renew":
		return runRenew(ctx, args[1:])
	case "version":
		fmt.Println("innerwall-agent", version)
		return nil
	case "-h", "--help", "help":
		usage()
		return flag.ErrHelp
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}
