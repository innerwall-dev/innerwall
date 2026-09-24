package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/store"
)

// labelFlags collects repeated --label key=value flags.
type labelFlags []enroll.Label

func (l *labelFlags) String() string {
	parts := make([]string, 0, len(*l))
	for _, x := range *l {
		parts = append(parts, x.Key+"="+x.Value)
	}
	return strings.Join(parts, ",")
}

func (l *labelFlags) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return fmt.Errorf("label %q is not key=value", s)
	}
	*l = append(*l, enroll.Label{Key: k, Value: v})
	return nil
}

func runToken(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: innerwall token mint|list|revoke [flags]")
	}
	switch args[0] {
	case "mint":
		return runTokenMint(ctx, args[1:])
	case "list":
		return runTokenList(ctx, args[1:])
	case "revoke":
		return runTokenRevoke(ctx, args[1:])
	default:
		return fmt.Errorf("unknown token command %q (mint|list|revoke)", args[0])
	}
}

func openStore(ctx context.Context, flagURL string) (*store.Store, error) {
	url, err := databaseURL(flagURL)
	if err != nil {
		return nil, err
	}
	return store.Open(ctx, url)
}

func runTokenMint(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall token mint", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	name := fs.String("name", "", "operator-facing name for the token")
	ttl := fs.Duration("ttl", enroll.DefaultTokenTTL, "token lifetime")
	var labels labelFlags
	fs.Var(&labels, "label", "label assigned to every workload the token enrolls, key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := &enroll.Service{Store: st}
	plaintext, tok, err := svc.MintToken(ctx, *name, labels, *ttl)
	if err != nil {
		return err
	}
	// The plaintext is printed exactly once, here, to stdout. It is not
	// stored and not logged; only its hash is persisted.
	fmt.Fprintf(os.Stderr, "token %s (%s) expires %s labels [%s]\n", tok.ID, tok.Name, tok.ExpiresAt.UTC().Format(time.RFC3339), labels.String())
	fmt.Fprintln(os.Stderr, "the token below is shown once; store it with the care of a password:")
	fmt.Println(plaintext)
	return nil
}

func runTokenList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall token list", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := &enroll.Service{Store: st}
	tokens, err := svc.ListTokens(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tPREFIX\tNAME\tSTATE\tEXPIRES\tUSES\tLABELS")
	for i := range tokens {
		t := &tokens[i]
		state := "valid"
		switch err := t.Check(now); {
		case err == nil:
		case errors.Is(err, enroll.ErrTokenRevoked):
			state = "revoked"
		case errors.Is(err, enroll.ErrTokenExpired):
			state = "expired"
		default:
			state = "invalid"
		}
		l := labelFlags(t.Labels)
		// A token minted before hints were kept has none to show.
		prefix := "-"
		if t.Prefix != nil {
			prefix = *t.Prefix
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", t.ID, prefix, t.Name, state, t.ExpiresAt.UTC().Format(time.RFC3339), t.UseCount, l.String())
	}
	return w.Flush()
}

func runTokenRevoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall token revoke", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: innerwall token revoke <token-id>")
	}
	id, err := uuid.Parse(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("token id: %w", err)
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := &enroll.Service{Store: st}
	if err := svc.RevokeToken(ctx, id); err != nil {
		return err
	}
	fmt.Printf("token %s revoked\n", id)
	return nil
}
