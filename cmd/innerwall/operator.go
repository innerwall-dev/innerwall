package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"golang.org/x/term"

	"github.com/innerwall-dev/innerwall/internal/operator"
)

// The operator commands are the command-line half of the operator surface
// (ADR-0021). They call the same domain functions the REST façade calls,
// authenticated by database credentials rather than operator credentials:
// setting the first password has to work on a host whose control plane is
// not running, and there is deliberately no endpoint that does it.

func runOperator(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall operator set-password|token [flags]")
	}
	switch args[0] {
	case "set-password":
		return runOperatorSetPassword(ctx, args[1:])
	case "token":
		return runOperatorToken(ctx, args[1:])
	default:
		return usageError("unknown operator command %q (set-password|token)", args[0])
	}
}

func runOperatorSetPassword(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall operator set-password", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	name := fs.String("name", "", "display name shown by the console (empty clears it)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	password, err := readPassword(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	var displayName *string
	if *name != "" {
		displayName = name
	}
	svc := &operator.Service{Store: st}
	if err := svc.SetPassword(ctx, password, displayName); err != nil {
		if errors.Is(err, operator.ErrPasswordTooShort) {
			return fmt.Errorf("password must be at least %d characters", operator.MinPasswordLength)
		}
		return err
	}
	fmt.Fprintln(os.Stderr, "operator password set")
	return nil
}

// readPassword reads the new password without echo when stdin is a
// terminal, asking twice so a typo is caught before it is hashed; when
// stdin is a pipe it reads one line, so provisioning can supply it.
// The password never appears on the command line or in shell history.
func readPassword(in *os.File, prompt io.Writer) (string, error) {
	if term.IsTerminal(int(in.Fd())) {
		_, _ = fmt.Fprint(prompt, "new password: ")
		first, err := term.ReadPassword(int(in.Fd()))
		_, _ = fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("reading password: %w", err)
		}
		_, _ = fmt.Fprint(prompt, "again: ")
		second, err := term.ReadPassword(int(in.Fd()))
		_, _ = fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("reading password: %w", err)
		}
		if string(first) != string(second) {
			return "", errors.New("passwords do not match")
		}
		return string(first), nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading password from stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func runOperatorToken(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall operator token mint|list|revoke [flags]")
	}
	switch args[0] {
	case "mint":
		return runOperatorTokenMint(ctx, args[1:])
	case "list":
		return runOperatorTokenList(ctx, args[1:])
	case "revoke":
		return runOperatorTokenRevoke(ctx, args[1:])
	default:
		return usageError("unknown operator token command %q (mint|list|revoke)", args[0])
	}
}

func runOperatorTokenMint(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall operator token mint", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	name := fs.String("name", "", "operator-facing name for the token")
	ttl := fs.Duration("ttl", 0, "token lifetime (0 means it does not expire)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := &operator.Service{Store: st}
	plaintext, tok, err := svc.MintToken(ctx, *name, *ttl)
	if err != nil {
		return err
	}
	// The plaintext is printed exactly once, here, to stdout. It is not
	// stored and not logged; only its hash is persisted.
	fmt.Fprintf(os.Stderr, "operator token %s (%s) expires %s\n", tok.ID, tok.Name, expiryString(tok.ExpiresAt))
	fmt.Fprintln(os.Stderr, "the token below is shown once; store it with the care of a password:")
	fmt.Println(plaintext)
	return nil
}

func expiryString(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.UTC().Format(time.RFC3339)
}

func runOperatorTokenList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall operator token list", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := &operator.Service{Store: st}
	tokens, err := svc.ListTokens(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tPREFIX\tNAME\tSTATE\tEXPIRES\tLAST USED")
	for i := range tokens {
		t := &tokens[i]
		state := "valid"
		switch err := t.Check(now); {
		case err == nil:
		case errors.Is(err, operator.ErrTokenRevoked):
			state = "revoked"
		case errors.Is(err, operator.ErrTokenExpired):
			state = "expired"
		default:
			state = "invalid"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s…\t%s\t%s\t%s\t%s\n", t.ID, t.Prefix, t.Name, state, expiryString(t.ExpiresAt), ago(t.LastUsedAt, now))
	}
	return w.Flush()
}

func runOperatorTokenRevoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall operator token revoke", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall operator token revoke <token-id>")
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

	svc := &operator.Service{Store: st}
	if err := svc.RevokeToken(ctx, id); err != nil {
		return err
	}
	fmt.Printf("operator token %s revoked\n", id)
	return nil
}
