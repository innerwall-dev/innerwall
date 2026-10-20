package operator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/authn"
	"github.com/innerwall-dev/innerwall/internal/operator"
)

// TestNewAuthenticatorComposesEveryForm pins the composition: one verifier
// for each credential form the surface accepts.
func TestNewAuthenticatorComposesEveryForm(t *testing.T) {
	svc, _, _ := newService(t)
	a := operator.NewAuthenticator(svc)
	var got []authn.Form
	for _, v := range a.Verifiers {
		got = append(got, v.Form())
	}
	want := []authn.Form{authn.Password, authn.OperatorToken, authn.Session}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("verifier forms %v, want %v", got, want)
	}
}

func TestPasswordVerifier(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	v := operator.PasswordVerifier{Service: svc}
	if _, err := v.Verify(ctx, "a perfectly fine password"); !errors.Is(err, operator.ErrNoOperator) {
		t.Fatalf("before a password is set: %v, want ErrNoOperator", err)
	}
	if err := svc.SetPassword(ctx, "a perfectly fine password", ptr("Ada")); err != nil {
		t.Fatal(err)
	}
	p, err := v.Verify(ctx, "a perfectly fine password")
	if err != nil || p.DisplayName == nil || *p.DisplayName != "Ada" {
		t.Fatalf("correct password: %+v, %v", p, err)
	}
	if p, err := v.Verify(ctx, "a perfectly wrong password"); !errors.Is(err, operator.ErrPasswordMismatch) || p != nil {
		t.Fatalf("wrong password: %+v, %v", p, err)
	}
}

func TestTokenVerifier(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	v := operator.TokenVerifier{Service: svc}
	plain, tok, err := svc.MintToken(ctx, "ci", 0)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := v.Verify(ctx, plain); err != nil || p == nil {
		t.Fatalf("live token: %+v, %v", p, err)
	}
	if _, err := v.Verify(ctx, "not-a-token"); !errors.Is(err, operator.ErrTokenMalformed) {
		t.Fatalf("malformed token: %v", err)
	}
	if err := svc.RevokeToken(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if p, err := v.Verify(ctx, plain); !errors.Is(err, operator.ErrTokenRevoked) || p != nil {
		t.Fatalf("revoked token: %+v, %v", p, err)
	}
}

// TestSessionMinterAndVerifier pins the session's round trip through the
// boundary: a minted session carries the domain's fixed lifetime and
// verifies until its lifetime runs out or it is ended.
func TestSessionMinterAndVerifier(t *testing.T) {
	svc, _, now := newService(t)
	ctx := context.Background()
	if err := svc.SetPassword(ctx, "a perfectly fine password", ptr("Ada")); err != nil {
		t.Fatal(err)
	}
	m := operator.SessionMinter{Service: svc}
	v := operator.SessionVerifier{Service: svc}

	sess, err := m.Mint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Lifetime != operator.DefaultSessionTTL {
		t.Fatalf("lifetime %v, want %v", sess.Lifetime, operator.DefaultSessionTTL)
	}
	p, err := v.Verify(ctx, sess.ID)
	if err != nil || p.DisplayName == nil || *p.DisplayName != "Ada" {
		t.Fatalf("minted session: %+v, %v", p, err)
	}
	if _, err := v.Verify(ctx, "nope"); !errors.Is(err, operator.ErrSessionMalformed) {
		t.Fatalf("malformed session: %v", err)
	}

	*now = now.Add(operator.DefaultSessionTTL)
	if _, err := v.Verify(ctx, sess.ID); !errors.Is(err, operator.ErrSessionExpired) {
		t.Fatalf("session at its expiry: %v", err)
	}

	ended, err := m.Mint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.End(ctx, ended.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(ctx, ended.ID); !errors.Is(err, operator.ErrSessionUnknown) {
		t.Fatalf("ended session: %v", err)
	}
	if err := m.End(ctx, "garbage"); err != nil {
		t.Fatalf("ending a malformed identifier: %v", err)
	}
}
