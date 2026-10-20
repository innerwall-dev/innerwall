package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
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
	// A document that is not there fails as it is read, naming the path.
	missing := filepath.Join(t.TempDir(), "missing.json")
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
		{"ruleset update", func(ctx context.Context, args []string) error { return runRulesetWrite(ctx, "update", args) }, []string{"web-to-db"}, []string{"-f", missing}, "open " + missing + ": no such file"},
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

// TestRulesetUpdateRefusesWhatWasIgnored pins ruleset update's one
// asymmetry. Earlier versions read its flags only before the ruleset and,
// given more than one positional, ignored every one when the document
// carried its own id; each such invocation is now refused, naming what was
// ignored, never read another way. None reaches the document or the
// database.
func TestRulesetUpdateRefusesWhatWasIgnored(t *testing.T) {
	t.Setenv(envDatabaseURL, "")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-f", "a.json", "web-to-db", "extra"}, `refusing "extra" after web-to-db: with -f before the ruleset`},
		{[]string{"-f", "a.json", "web-to-db", "--database-url", "X"}, `refusing "--database-url" "X" after web-to-db: with -f before the ruleset`},
		{[]string{"-f", "a.json", "web-to-db", "-f", "b.json"}, `refusing "-f" "b.json" after web-to-db: with -f before the ruleset`},
		{[]string{"-f", "a.json", "web-to-db", "--"}, `refusing "--" after web-to-db: with -f before the ruleset`},
		{[]string{"-f", "a.json", "--", "web-to-db", "extra"}, `refusing "extra" after web-to-db: with -f before the ruleset`},
		{[]string{"web-to-db", "extra", "-f", "a.json"}, `ruleset update takes one ruleset: refusing "extra" "-f" "a.json" after web-to-db`},
	} {
		err := runRulesetWrite(context.Background(), "update", tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ruleset update %q: %v, want an error containing %q", tc.args, err, tc.want)
		}
	}
}
