// Package operator is the operator-surface authentication domain (ADR-0021):
// the single operator's password, browser sessions, and operator tokens.
// It is transport-agnostic; the command line and the REST façade are two
// callers of the same functions. Persistence is behind Store, implemented
// with hand-written SQL in internal/store (ADR-0006).
//
// Version 1 has one operator and binary authorization: a caller is
// authenticated or it is not. Both credential forms (a session from a
// password login, or a bearer token) resolve to the same Principal, and
// nothing downstream can tell which one was presented.
package operator

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Errors the domain reports. They are distinct so an operator sees exactly
// why a credential was refused; none of them reveals anything a caller
// without the credential could not already guess.
var (
	// ErrNoOperator means no password has ever been set. The surface maps
	// it to a distinct problem type so a fresh install is recognizable.
	ErrNoOperator = errors.New("operator: no password has been set")
	// ErrPasswordMismatch means the password did not verify.
	ErrPasswordMismatch = errors.New("operator: password does not match")
	// ErrPasswordTooShort means a new password is below MinPasswordLength.
	ErrPasswordTooShort = errors.New("operator: password is too short")

	ErrSessionMalformed = errors.New("operator: session identifier is malformed")
	ErrSessionUnknown   = errors.New("operator: session is not recognized")
	ErrSessionExpired   = errors.New("operator: session has expired")

	ErrTokenMalformed = errors.New("operator: operator token is malformed")
	ErrTokenUnknown   = errors.New("operator: operator token is not recognized")
	ErrTokenExpired   = errors.New("operator: operator token has expired")
	ErrTokenRevoked   = errors.New("operator: operator token has been revoked")
)

// Operator is the single operator row: the credential and the attributes
// beside it. DisplayName is optional; a surface renders a fallback when it
// is nil.
type Operator struct {
	PasswordHash string
	DisplayName  *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session is the stored form of a browser session. IDHash is the SHA-256
// digest of the identifier the browser holds; the identifier itself is
// never stored.
type Session struct {
	IDHash    []byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Token is the stored form of an operator token. It never carries the
// plaintext: Hash is what is looked up, Prefix is what is listed.
type Token struct {
	ID         uuid.UUID
	Hash       []byte
	Prefix     string
	Name       string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
}

// Principal is the authenticated operator as handlers see it. It carries no
// record of which credential form produced it.
type Principal struct {
	DisplayName *string
}

// Store is the persistence the domain needs.
type Store interface {
	// GetOperator returns ErrNoOperator when no password has been set.
	GetOperator(ctx context.Context) (*Operator, error)
	// SetOperator creates or replaces the single operator row.
	SetOperator(ctx context.Context, op Operator) error

	// CreateSession persists a session (hash only).
	CreateSession(ctx context.Context, s Session) error
	// GetSession returns ErrSessionUnknown when no session has that hash.
	// Expiry is the domain's judgement, not the store's.
	GetSession(ctx context.Context, idHash []byte) (*Session, error)
	// DeleteSession removes a session; an unknown hash is not an error.
	DeleteSession(ctx context.Context, idHash []byte) error
	// DeleteExpiredSessions removes every session whose expiry is at or
	// before now and reports how many.
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)

	// CreateOperatorToken persists a minted token (hash only).
	CreateOperatorToken(ctx context.Context, t Token) error
	// FindOperatorTokenByHash returns ErrTokenUnknown when no token has that hash.
	FindOperatorTokenByHash(ctx context.Context, hash []byte) (*Token, error)
	// ListOperatorTokens returns every token, newest first.
	ListOperatorTokens(ctx context.Context) ([]Token, error)
	// RevokeOperatorToken marks a token revoked at instant now. It returns
	// ErrTokenUnknown for an unknown id and ErrTokenRevoked if it already was.
	RevokeOperatorToken(ctx context.Context, id uuid.UUID, now time.Time) error
	// RecordOperatorTokenUse stores the instant a token last authenticated a call.
	RecordOperatorTokenUse(ctx context.Context, id uuid.UUID, now time.Time) error
}

// Check reports whether the session is live at instant now.
func (s *Session) Check(now time.Time) error {
	if s == nil {
		return ErrSessionUnknown
	}
	if !s.ExpiresAt.After(now) {
		return ErrSessionExpired
	}
	return nil
}

// Check reports whether the token may authenticate at instant now.
// Revocation is checked before expiry so a revoked token reports as revoked
// even after it would have expired anyway; the operator's action is the
// more specific fact.
func (t *Token) Check(now time.Time) error {
	if t == nil {
		return ErrTokenUnknown
	}
	if t.RevokedAt != nil && !t.RevokedAt.After(now) {
		return ErrTokenRevoked
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.After(now) {
		return ErrTokenExpired
	}
	return nil
}
