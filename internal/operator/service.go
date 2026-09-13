package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DefaultSessionTTL is the fixed lifetime of a session. It is set at
// creation and never extended: the longest a leaked cookie stays useful is
// bounded by the clock, not by activity (ADR-0021).
const DefaultSessionTTL = 7 * 24 * time.Hour

// Service is the operator authentication domain.
type Service struct {
	Store Store
	// SessionTTL is the lifetime of a session; DefaultSessionTTL if zero.
	SessionTTL time.Duration
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) sessionTTL() time.Duration {
	if s.SessionTTL > 0 {
		return s.SessionTTL
	}
	return DefaultSessionTTL
}

// SetPassword creates the operator or replaces its password, and sets the
// display name (nil clears it). It refuses a password shorter than
// MinPasswordLength.
func (s *Service) SetPassword(ctx context.Context, password string, displayName *string) error {
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	now := s.now()
	return s.Store.SetOperator(ctx, Operator{PasswordHash: hash, DisplayName: displayName, CreatedAt: now, UpdatedAt: now})
}

// VerifyPassword checks password against the stored hash. It returns
// ErrNoOperator when no password has been set and ErrPasswordMismatch when
// it does not verify.
func (s *Service) VerifyPassword(ctx context.Context, password string) error {
	op, err := s.Store.GetOperator(ctx)
	if err != nil {
		return err
	}
	return VerifyPassword(op.PasswordHash, password)
}

// CreateSession draws a session and persists its hash. The plaintext is
// returned once, for the cookie.
func (s *Service) CreateSession(ctx context.Context) (plaintext string, sess Session, err error) {
	plaintext, hash, err := NewSessionID()
	if err != nil {
		return "", Session{}, err
	}
	now := s.now()
	sess = Session{IDHash: hash, CreatedAt: now, ExpiresAt: now.Add(s.sessionTTL())}
	if err := s.Store.CreateSession(ctx, sess); err != nil {
		return "", Session{}, err
	}
	return plaintext, sess, nil
}

// ValidateSession resolves a session identifier to the principal. Expiry is
// judged here, at the instant of use, so a session past its lifetime is
// refused whether or not retention has pruned it yet.
func (s *Service) ValidateSession(ctx context.Context, plaintext string) (*Principal, error) {
	hash, err := HashSessionID(plaintext)
	if err != nil {
		return nil, err
	}
	sess, err := s.Store.GetSession(ctx, hash)
	if err != nil {
		return nil, err
	}
	if err := sess.Check(s.now()); err != nil {
		return nil, err
	}
	return s.principal(ctx)
}

// DeleteSession revokes a session. A malformed or unknown identifier is
// not an error: the outcome the caller wants, that the identifier no longer
// authenticates, already holds.
func (s *Service) DeleteSession(ctx context.Context, plaintext string) error {
	hash, err := HashSessionID(plaintext)
	if err != nil {
		return nil
	}
	return s.Store.DeleteSession(ctx, hash)
}

// PruneSessions deletes expired sessions. It rides the retention schedule
// the flow store runs (ADR-0021); there is no job of its own.
func (s *Service) PruneSessions(ctx context.Context, now time.Time) (int64, error) {
	return s.Store.DeleteExpiredSessions(ctx, now)
}

// MintToken creates an operator token with the given lifetime (none if ttl
// is zero) and returns the plaintext exactly once.
func (s *Service) MintToken(ctx context.Context, name string, ttl time.Duration) (plaintext string, tok Token, err error) {
	plaintext, hash, prefix, err := NewToken()
	if err != nil {
		return "", Token{}, err
	}
	now := s.now()
	tok = Token{ID: uuid.New(), Hash: hash, Prefix: prefix, Name: name, CreatedAt: now}
	if ttl > 0 {
		exp := now.Add(ttl)
		tok.ExpiresAt = &exp
	}
	if err := s.Store.CreateOperatorToken(ctx, tok); err != nil {
		return "", Token{}, err
	}
	return plaintext, tok, nil
}

// ListTokens returns every token, newest first.
func (s *Service) ListTokens(ctx context.Context) ([]Token, error) {
	return s.Store.ListOperatorTokens(ctx)
}

// RevokeToken revokes a token as of now.
func (s *Service) RevokeToken(ctx context.Context, id uuid.UUID) error {
	return s.Store.RevokeOperatorToken(ctx, id, s.now())
}

// ValidateToken resolves a bearer token to the principal and records the
// use. The token is checked before anything else is read, so a caller
// without a valid token learns nothing.
func (s *Service) ValidateToken(ctx context.Context, plaintext string) (*Principal, error) {
	hash, err := HashToken(plaintext)
	if err != nil {
		return nil, err
	}
	tok, err := s.Store.FindOperatorTokenByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := tok.Check(now); err != nil {
		return nil, err
	}
	if err := s.Store.RecordOperatorTokenUse(ctx, tok.ID, now); err != nil {
		return nil, fmt.Errorf("operator: recording token use: %w", err)
	}
	return s.principal(ctx)
}

// principal builds the one principal. A token can exist before a password
// has been set; the principal then simply has no display name.
func (s *Service) principal(ctx context.Context) (*Principal, error) {
	op, err := s.Store.GetOperator(ctx)
	if errors.Is(err, ErrNoOperator) {
		return &Principal{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &Principal{DisplayName: op.DisplayName}, nil
}
