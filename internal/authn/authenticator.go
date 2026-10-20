package authn

import (
	"context"
	"errors"
	"time"
)

// Form names a credential form. Forms exist only at this boundary: a
// verifier is chosen by the form presented, and the principal it resolves
// never records which one it was.
type Form int

const (
	// Password is the operator password, presented once to log in.
	Password Form = iota + 1
	// OperatorToken is a bearer operator token, presented by automation.
	OperatorToken
	// Session is a session identifier a login minted, presented by a
	// browser in its cookie.
	Session
)

// Credential is one presented credential: its form and its secret.
type Credential struct {
	Form   Form
	Secret string
}

// ErrUnsupportedForm means no verifier accepts the credential's form.
var ErrUnsupportedForm = errors.New("authn: no verifier accepts this credential form")

// Verifier checks one credential form and resolves it to the principal.
// A refusal is the verifier's own error, which callers match with
// errors.Is; a verifier never falls back to another form.
type Verifier interface {
	// Form is the credential form this verifier accepts.
	Form() Form
	// Verify resolves secret to the principal, or refuses it.
	Verify(ctx context.Context, secret string) (*Principal, error)
}

// MintedSession is a session a login minted: the identifier the client
// holds, returned exactly once, and its fixed lifetime.
type MintedSession struct {
	ID       string
	Lifetime time.Duration
}

// Minter mints sessions and ends them. A session is minted only after a
// credential has been verified; the minter does not verify.
type Minter interface {
	// Mint draws and persists a new session.
	Mint(ctx context.Context) (MintedSession, error)
	// End revokes a session. An identifier that no longer authenticates
	// is not an error: that is the outcome the caller wants.
	End(ctx context.Context, id string) error
}

// Authenticator is the boundary the operator surface is written against:
// credential verification by form, over one session minter. The operator
// domain's implementation is operator.NewAuthenticator; a backend that
// verifies elsewhere replaces it without touching the surface.
type Authenticator interface {
	// Verify resolves a credential to the principal through the verifier
	// for its form, or reports ErrUnsupportedForm when there is none.
	Verify(ctx context.Context, c Credential) (*Principal, error)
	Minter
}

// Chain is an Authenticator composed from a list of verifiers over one
// minter. The first verifier whose form matches the credential decides.
type Chain struct {
	Verifiers []Verifier
	Minter    Minter
}

// Verify implements Authenticator.
func (c *Chain) Verify(ctx context.Context, cred Credential) (*Principal, error) {
	for _, v := range c.Verifiers {
		if v.Form() == cred.Form {
			return v.Verify(ctx, cred.Secret)
		}
	}
	return nil, ErrUnsupportedForm
}

// Mint implements Minter.
func (c *Chain) Mint(ctx context.Context) (MintedSession, error) { return c.Minter.Mint(ctx) }

// End implements Minter.
func (c *Chain) End(ctx context.Context, id string) error { return c.Minter.End(ctx, id) }
