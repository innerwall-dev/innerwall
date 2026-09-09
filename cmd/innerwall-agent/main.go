// Command innerwall-agent runs on each workload. It observes flows and
// programs the host's native firewall from desired state pushed by the control
// plane. It dials out only, never listens, never self-updates, and fails
// static: losing the control plane changes nothing on the host (ADR-0011).
//
// Milestone M1 ships the shape of the binary only. The four loops (sync,
// collect, reconcile, health) are wired in from M2 onward.
package main

import (
	"fmt"
)

// version is set at link time by the release build (see .goreleaser.yaml).
var version = "dev"

func main() {
	fmt.Printf("innerwall-agent %s\n", version)
}
