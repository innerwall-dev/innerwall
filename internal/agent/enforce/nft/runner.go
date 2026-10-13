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
// table as nft prints it, for diagnostics and tests. Tables returns the
// names of the inet tables the kernel holds, which is how the agent tells
// whether its own is still there; it reads and changes nothing else.
type Runner interface {
	Apply(ctx context.Context, script string) error
	List(ctx context.Context, table string) (string, error)
	Tables(ctx context.Context) ([]string, error)
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

// Tables implements Runner.
func (r ExecRunner) Tables(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, r.binary(), "list", "tables", "inet") //nolint:gosec // the binary is operator configuration
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nft: listing tables: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseTables(stdout.String()), nil
}

// parseTables reads the names out of `nft list tables inet`, one
// "table inet <name>" per line.
func parseTables(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "table" && f[1] == "inet" {
			names = append(names, f[2])
		}
	}
	return names
}
