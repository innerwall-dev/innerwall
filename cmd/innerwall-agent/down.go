package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce/nft"
)

// runDown is the local kill switch (ADR-0011): it deletes the owned
// nftables table and nothing else, without the control plane. Root on the
// host outranks the platform, and the command says exactly what it did
// and what happens next.
func runDown(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall-agent down", flag.ContinueOnError)
	stateDir := fs.String("state-dir", stateDirDefault(), "directory holding the last applied policy")
	table := fs.String("nft-table", nft.DefaultTable, "name of the owned nftables table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := nft.Teardown(ctx, nil, nft.Options{Table: *table}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ENFORCEMENT REMOVED FROM THIS HOST.\n")
	fmt.Fprintf(os.Stderr, "The owned nftables table %q was deleted. This agent no longer evaluates or drops any inbound connection; every other firewall table on the host is untouched.\n", *table)
	persisted, err := enforce.PolicyFile{Path: enforce.PolicyPath(*stateDir)}.Load()
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "The persisted policy could not be read (%v); a running or restarted daemon applies the next snapshot it receives.\n", err)
	case persisted == nil:
		fmt.Fprintf(os.Stderr, "No persisted policy is on disk; a running or restarted daemon applies the next snapshot it receives.\n")
	default:
		fmt.Fprintf(os.Stderr, "The persisted policy (version %d, mode %s, %d rules) stays on disk: the next daemon start re-applies it, and a daemon that is still running re-applies on its next policy update. To keep enforcement off, stop the daemon too.\n",
			persisted.GetVersion(), strings.ToLower(strings.TrimPrefix(persisted.GetMode().String(), "ENFORCEMENT_MODE_")), len(persisted.GetInboundRules()))
	}
	return nil
}
