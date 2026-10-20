package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/policy"
)

// TestCommandsReadFlagsOnEitherSide pins that the commands that take a
// positional argument read their flags on either side of it, and read them
// the same way. Each flag set is chosen to fail before or at the database,
// with an error that names the flag's value, so no Postgres is needed and a
// flag left unread shows as a different error.
func TestCommandsReadFlagsOnEitherSide(t *testing.T) {
	t.Setenv(envDatabaseURL, "")
	const id = "8d1f4c1e-0000-4000-8000-000000000001"
	// An unparseable connection string fails when the pool is opened,
	// before anything dials.
	badDB := []string{"--database-url", "postgres://%zz"}
	const atDB = "store: opening pool"
	for _, tc := range []struct {
		name  string
		run   func(context.Context, []string) error
		pos   []string
		flags []string
		want  string
	}{
		{"flows list", runFlowsList, []string{id}, []string{"--since", "1h", "--decision", "nope"}, `unknown verdict "nope"`},
		{"flows totals", runFlowsTotals, []string{id}, []string{"--decision", "nope"}, `unknown verdict "nope"`},
		{"workload set-labels", runWorkloadSetLabels, []string{id}, append([]string{"--label", "app=web", "--label", "env=prod"}, badDB...), atDB},
		{"workload set-mode", runWorkloadSetMode, []string{id, "simulation"}, badDB, atDB},
		{"service update", func(ctx context.Context, args []string) error { return runServiceWrite(ctx, "update", args) }, []string{"postgres"}, append([]string{"--entry", "tcp:5432"}, badDB...), atDB},
		{"address-group update", func(ctx context.Context, args []string) error { return runAddressGroupWrite(ctx, "update", args) }, []string{"corp"}, append([]string{"--cidr", "10.0.0.0/8"}, badDB...), atDB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			orders := map[string][]string{
				"flags first": append(append([]string{}, tc.flags...), tc.pos...),
				"flags after": append(append([]string{}, tc.pos...), tc.flags...),
			}
			// Between two positionals, as well.
			if len(tc.pos) > 1 {
				orders["flags between"] = append(append(append([]string{}, tc.pos[:1]...), tc.flags...), tc.pos[1:]...)
			}
			for order, args := range orders {
				err := tc.run(ctx, args)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("%s %q: %v, want an error containing %q", order, args, err, tc.want)
				}
			}
		})
	}
}

// TestCommandsKeepRefusingExtraPositionals pins that a positional past a
// command's count still ends its flags, so the command refuses it as before
// instead of reading the flags after it.
func TestCommandsKeepRefusingExtraPositionals(t *testing.T) {
	t.Setenv(envDatabaseURL, "")
	const id = "8d1f4c1e-0000-4000-8000-000000000001"
	err := runFlowsList(context.Background(), []string{id, "extra", "--decision", "nope"})
	if err == nil || !strings.HasPrefix(err.Error(), "usage: innerwall flows list") {
		t.Errorf("flows list with an extra positional: %v, want its usage line", err)
	}
}

// TestRulesetUpdateReadsFlagsBeforeItsRuleset pins that ruleset update
// still reads its flags only before the ruleset it names: an update given
// more than one positional ignores them all when the document carries its
// own id, so a flag among them is not read.
func TestRulesetUpdateReadsFlagsBeforeItsRuleset(t *testing.T) {
	t.Setenv(envDatabaseURL, "")
	err := runRulesetWrite(context.Background(), "update", []string{"web-to-db", "-f", "ruleset.json"})
	if !errors.Is(err, policy.ErrNoDocument) {
		t.Errorf("ruleset update <name> -f <file>: %v, want %v", err, policy.ErrNoDocument)
	}
}
