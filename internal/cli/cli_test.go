package cli

import (
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"
)

// labels is a repeatable flag, as the command lines' --label is.
type labels []string

func (l *labels) String() string     { return strings.Join(*l, ",") }
func (l *labels) Set(v string) error { *l = append(*l, v); return nil }

type parsed struct {
	since  string
	json   bool
	labels []string
	args   []string
}

func newFlags() (*flag.FlagSet, *parsed) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	p := &parsed{}
	fs.StringVar(&p.since, "since", "24h", "")
	fs.BoolVar(&p.json, "json", false, "")
	fs.Var((*labels)(&p.labels), "label", "")
	return fs, p
}

func run(args []string, maxPositional int) (*parsed, error) {
	fs, p := newFlags()
	err := Parse(fs, args, maxPositional)
	p.args = fs.Args()
	if len(p.args) == 0 {
		p.args = nil
	}
	return p, err
}

func TestParse(t *testing.T) {
	const id = "8d1f4c1e-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name string
		args []string
		max  int
		want parsed
	}{
		{"flags first", []string{"--since", "1h", id}, 1, parsed{since: "1h", args: []string{id}}},
		{"flag after the positional", []string{id, "--since", "1h"}, 1, parsed{since: "1h", args: []string{id}}},
		{"flags on both sides", []string{"--label", "a=b", id, "--label", "c=d", "--json"}, 1, parsed{since: "24h", json: true, labels: []string{"a=b", "c=d"}, args: []string{id}}},
		{"between two positionals", []string{id, "--since=2h", "simulation"}, 2, parsed{since: "2h", args: []string{id, "simulation"}}},
		{"one dash", []string{id, "-since", "1h"}, 1, parsed{since: "1h", args: []string{id}}},
		{"-- ends flags before the positional", []string{"--", "--since"}, 1, parsed{since: "24h", args: []string{"--since"}}},
		{"-- ends flags after the positional", []string{id, "--json", "--", "--since", "1h"}, 1, parsed{since: "24h", json: true, args: []string{id, "--since", "1h"}}},
		{"flag-shaped value", []string{id, "--since", "-1h"}, 1, parsed{since: "-1h", args: []string{id}}},
		{"-- as a value", []string{"--since", "--", id}, 1, parsed{since: "--", args: []string{id}}},
		{"a lone dash is positional", []string{"-", "--json"}, 1, parsed{since: "24h", json: true, args: []string{"-"}}},
		{"a boolean flag takes no value", []string{"--json", id}, 1, parsed{since: "24h", json: true, args: []string{id}}},
		{"a positional past the count ends flags", []string{id, "extra", "--since", "1h"}, 1, parsed{since: "24h", args: []string{id, "extra", "--since", "1h"}}},
		{"none taken: the first positional ends flags", []string{"--json", "stray", "--since", "1h"}, 0, parsed{since: "24h", json: true, args: []string{"stray", "--since", "1h"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := run(tc.args, tc.max)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.args, err)
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("Parse(%q) = %+v, want %+v", tc.args, *got, tc.want)
			}
		})
	}
}

func TestParseRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"an undefined flag after the positional", []string{"id", "--nope"}, "flag provided but not defined: -nope"},
		{"a value missing at the end", []string{"id", "--since"}, "flag needs an argument: -since"},
		{"bad flag syntax", []string{"id", "---since", "1h"}, "bad flag syntax: ---since"},
		{"a bad boolean", []string{"id", "--json=maybe"}, `invalid boolean value "maybe" for -json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := run(tc.args, 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse(%q) = %v, want an error containing %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestParseAddsOnly holds the helper to fs.Parse wherever fs.Parse already
// read the arguments in full: with no positional taken, on every input;
// and with any count, when no flag follows a positional.
func TestParseAddsOnly(t *testing.T) {
	for _, tc := range []struct {
		args []string
		// a flag-shaped argument follows a positional under fs.Parse
		flagAfterPositional bool
	}{
		{nil, false},
		{[]string{"id"}, false},
		{[]string{"--json", "id"}, false},
		{[]string{"--since", "1h", "--label", "a=b", "id", "mode"}, false},
		{[]string{"--", "--json", "id"}, false},
		{[]string{"--since", "--", "id"}, false},
		{[]string{"--json=false", "-", "id"}, false},
		{[]string{"--since"}, false},
		{[]string{"--nope", "id"}, false},
		{[]string{"id", "--json"}, true},
		{[]string{"id", "--", "x"}, true},
	} {
		args := tc.args
		fs, want := newFlags()
		wantErr := fs.Parse(args)
		want.args = fs.Args()
		if len(want.args) == 0 {
			want.args = nil
		}
		for maxPositional := 0; maxPositional <= 2; maxPositional++ {
			if maxPositional > 0 && tc.flagAfterPositional {
				continue
			}
			got, err := run(args, maxPositional)
			if (err == nil) != (wantErr == nil) || (err != nil && err.Error() != wantErr.Error()) {
				t.Errorf("Parse(%q, %d) error %v, fs.Parse error %v", args, maxPositional, err, wantErr)
				continue
			}
			if err == nil && !reflect.DeepEqual(*got, *want) {
				t.Errorf("Parse(%q, %d) = %+v, fs.Parse = %+v", args, maxPositional, *got, *want)
			}
		}
	}
}

func TestLeading(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		wantNames []string
		wantRest  []string
	}{
		{"flags then positionals", []string{"--since", "1h", "--json", "id", "--label", "a=b"}, []string{"since", "json"}, []string{"id", "--label", "a=b"}},
		{"one dash and =", []string{"-since=1h", "id"}, []string{"since"}, []string{"id"}},
		{"-- ends the flags and is left out", []string{"--json", "--", "id", "x"}, []string{"json"}, []string{"id", "x"}},
		{"a flag-shaped value is a value", []string{"--since", "-f", "id"}, []string{"since"}, []string{"id"}},
		{"no positional", []string{"--json"}, []string{"json"}, nil},
		{"a value missing at the end", []string{"--since"}, []string{"since"}, nil},
		{"positional first", []string{"id", "--json"}, nil, []string{"id", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, p := newFlags()
			names, rest := Leading(fs, tc.args)
			if !reflect.DeepEqual(names, tc.wantNames) || !reflect.DeepEqual(rest, tc.wantRest) {
				t.Errorf("Leading(%q) = %q, %q, want %q, %q", tc.args, names, rest, tc.wantNames, tc.wantRest)
			}
			if p.since != "24h" || p.json || p.labels != nil {
				t.Errorf("Leading(%q) read flags into the set", tc.args)
			}
		})
	}
}
