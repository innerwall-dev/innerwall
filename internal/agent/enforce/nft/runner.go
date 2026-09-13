package nft

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes nft scripts. Apply hands one script to `nft -f`, which
// commits the whole script as a single transaction: on any error the
// kernel's ruleset is exactly what it was before. List returns the owned
// table as nft prints it, for diagnostics and tests.
type Runner interface {
	Apply(ctx context.Context, script string) error
	List(ctx context.Context, table string) (string, error)
}

// ExecRunner runs the nft binary.
type ExecRunner struct {
	// Binary is the nft executable; "nft" on PATH when empty.
	Binary string
}

var _ Runner = ExecRunner{}

func (r ExecRunner) binary() string {
	if r.Binary != "" {
		return r.Binary
	}
	return "nft"
}

// Apply implements Runner.
func (r ExecRunner) Apply(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, r.binary(), "-f", "-") //nolint:gosec // the binary is operator configuration, the script is ours
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nft: applying ruleset: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// List implements Runner.
func (r ExecRunner) List(ctx context.Context, table string) (string, error) {
	cmd := exec.CommandContext(ctx, r.binary(), "list", "table", "inet", table) //nolint:gosec // the binary is operator configuration
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("nft: listing table %s: %w: %s", table, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
