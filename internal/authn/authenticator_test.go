package authn_test

import (
	"context"
	"errors"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/authn"
)

// verifier accepts exactly one secret of its form.
type verifier struct {
	form   authn.Form
	secret string
	calls  *[]authn.Form
}

var errRefused = errors.New("refused")

func (v verifier) Form() authn.Form { return v.form }

func (v verifier) Verify(_ context.Context, secret string) (*authn.Principal, error) {
	*v.calls = append(*v.calls, v.form)
	if secret != v.secret {
		return nil, errRefused
	}
	return &authn.Principal{}, nil
}

type minter struct{ ended []string }

func (m *minter) Mint(context.Context) (authn.MintedSession, error) {
	return authn.MintedSession{ID: "minted"}, nil
}

func (m *minter) End(_ context.Context, id string) error {
	m.ended = append(m.ended, id)
	return nil
}

// TestChainVerifiesByForm pins how the verifier list is consulted: the
// verifier for the credential's form decides, its refusal is returned as
// it is, no other verifier is tried, and a form no verifier accepts is
// refused as such.
func TestChainVerifiesByForm(t *testing.T) {
	var calls []authn.Form
	c := &authn.Chain{Verifiers: []authn.Verifier{
		verifier{form: authn.Password, secret: "pw", calls: &calls},
		verifier{form: authn.OperatorToken, secret: "tok", calls: &calls},
	}, Minter: &minter{}}
	ctx := context.Background()

	if p, err := c.Verify(ctx, authn.Credential{Form: authn.OperatorToken, Secret: "tok"}); err != nil || p == nil {
		t.Fatalf("token: %v, %v", p, err)
	}
	if p, err := c.Verify(ctx, authn.Credential{Form: authn.Password, Secret: "tok"}); !errors.Is(err, errRefused) || p != nil {
		t.Fatalf("a token's secret presented as a password: %v, %v; want the password verifier's refusal", p, err)
	}
	if want := []authn.Form{authn.OperatorToken, authn.Password}; len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("verifiers consulted %v, want %v", calls, want)
	}
	if p, err := c.Verify(ctx, authn.Credential{Form: authn.Session, Secret: "pw"}); !errors.Is(err, authn.ErrUnsupportedForm) || p != nil {
		t.Fatalf("session with no session verifier: %v, %v", p, err)
	}
	if len(calls) != 2 {
		t.Fatalf("an unsupported form reached a verifier: %v", calls)
	}
}

// TestChainMintsThroughItsMinter pins that minting and ending a session
// are the minter's, unchanged.
func TestChainMintsThroughItsMinter(t *testing.T) {
	m := &minter{}
	c := &authn.Chain{Minter: m}
	sess, err := c.Mint(context.Background())
	if err != nil || sess.ID != "minted" {
		t.Fatalf("mint: %+v, %v", sess, err)
	}
	if err := c.End(context.Background(), sess.ID); err != nil || len(m.ended) != 1 || m.ended[0] != "minted" {
		t.Fatalf("end: %v, ended %v", err, m.ended)
	}
}
