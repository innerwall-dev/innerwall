// Command innerwall is the control plane: one binary containing the API
// service, agent gateway, policy compiler, flow ingestion, and embedded CA.
// All durable state lives in Postgres; replicas are stateless and
// interchangeable (ADR-0005).
//
// Milestone M1 ships the shape of the binary only. The services are wired in
// from M2 onward.
package main

import (
	"fmt"
	"os"

	"github.com/innerwall-dev/innerwall/ui"
)

// version is set at link time by the release build (see .goreleaser.yaml).
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "innerwall:", err)
		os.Exit(1)
	}
}

func run() error {
	entries, err := ui.Assets.ReadDir("dist")
	if err != nil {
		return fmt.Errorf("reading embedded ui assets: %w", err)
	}
	fmt.Printf("innerwall control plane %s (embedded ui entries: %d)\n", version, len(entries))
	return nil
}
