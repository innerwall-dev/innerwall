package operator

import (
	"context"

	"github.com/innerwall-dev/innerwall/internal/authn"
)

// NewAuthenticator composes the operator domain's verifiers, one per
// credential form, over its session minter.
func NewAuthenticator(s *Service) *authn.Chain {
	return &authn.Chain{
		Verifiers: []authn.Verifier{PasswordVerifier{s}, TokenVerifier{s}, SessionVerifier{s}},
		Minter:    SessionMinter{s},
	}
}

// PasswordVerifier verifies the operator password. It reports
// ErrNoOperator when no password has been set and ErrPasswordMismatch when
// it does not verify.
type PasswordVerifier struct{ Service *Service }

// Form implements authn.Verifier.
func (PasswordVerifier) Form() authn.Form { return authn.Password }

// Verify implements authn.Verifier.
func (v PasswordVerifier) Verify(ctx context.Context, password string) (*authn.Principal, error) {
	if err := v.Service.VerifyPassword(ctx, password); err != nil {
		return nil, err
	}
	return v.Service.principal(ctx)
}

// TokenVerifier verifies a bearer operator token and records its use.
type TokenVerifier struct{ Service *Service }

// Form implements authn.Verifier.
func (TokenVerifier) Form() authn.Form { return authn.OperatorToken }

// Verify implements authn.Verifier.
func (v TokenVerifier) Verify(ctx context.Context, token string) (*authn.Principal, error) {
	return v.Service.ValidateToken(ctx, token)
}

// SessionVerifier verifies a session identifier a login minted.
type SessionVerifier struct{ Service *Service }

// Form implements authn.Verifier.
func (SessionVerifier) Form() authn.Form { return authn.Session }

// Verify implements authn.Verifier.
func (v SessionVerifier) Verify(ctx context.Context, id string) (*authn.Principal, error) {
	return v.Service.ValidateSession(ctx, id)
}

// SessionMinter mints and ends sessions with the domain's fixed lifetime.
type SessionMinter struct{ Service *Service }

// Mint implements authn.Minter.
func (m SessionMinter) Mint(ctx context.Context) (authn.MintedSession, error) {
	id, sess, err := m.Service.CreateSession(ctx)
	if err != nil {
		return authn.MintedSession{}, err
	}
	return authn.MintedSession{ID: id, Lifetime: sess.ExpiresAt.Sub(sess.CreatedAt)}, nil
}

// End implements authn.Minter.
func (m SessionMinter) End(ctx context.Context, id string) error {
	return m.Service.DeleteSession(ctx, id)
}
