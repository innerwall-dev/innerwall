package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

var _ operator.Store = (*Store)(nil)

// --- operator --------------------------------------------------------------

// GetOperator implements operator.Store.
func (s *Store) GetOperator(ctx context.Context) (*operator.Operator, error) {
	row, err := s.q.GetOperator(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, operator.ErrNoOperator
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up operator: %w", err)
	}
	return &operator.Operator{PasswordHash: row.PasswordHash, DisplayName: row.DisplayName, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

// SetOperator implements operator.Store. The table holds one row; a second
// call replaces the credential and display name in place.
func (s *Store) SetOperator(ctx context.Context, op operator.Operator) error {
	if _, err := s.q.SetOperator(ctx, db.SetOperatorParams{PasswordHash: op.PasswordHash, DisplayName: op.DisplayName, CreatedAt: op.UpdatedAt}); err != nil {
		return fmt.Errorf("store: setting operator: %w", err)
	}
	return nil
}

// --- sessions --------------------------------------------------------------

// CreateSession implements operator.Store.
func (s *Store) CreateSession(ctx context.Context, sess operator.Session) error {
	if err := s.q.CreateOperatorSession(ctx, db.CreateOperatorSessionParams{IDHash: sess.IDHash, CreatedAt: sess.CreatedAt, ExpiresAt: sess.ExpiresAt}); err != nil {
		return fmt.Errorf("store: creating session: %w", err)
	}
	return nil
}

// GetSession implements operator.Store.
func (s *Store) GetSession(ctx context.Context, idHash []byte) (*operator.Session, error) {
	row, err := s.q.GetOperatorSession(ctx, idHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, operator.ErrSessionUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up session: %w", err)
	}
	return &operator.Session{IDHash: row.IDHash, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}, nil
}

// DeleteSession implements operator.Store.
func (s *Store) DeleteSession(ctx context.Context, idHash []byte) error {
	if _, err := s.q.DeleteOperatorSession(ctx, idHash); err != nil {
		return fmt.Errorf("store: deleting session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions implements operator.Store.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.q.DeleteExpiredOperatorSessions(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("store: pruning sessions: %w", err)
	}
	return n, nil
}

// --- operator tokens -------------------------------------------------------

// CreateOperatorToken implements operator.Store.
func (s *Store) CreateOperatorToken(ctx context.Context, t operator.Token) error {
	if err := s.q.CreateOperatorToken(ctx, db.CreateOperatorTokenParams{
		ID:          t.ID,
		TokenHash:   t.Hash,
		TokenPrefix: t.Prefix,
		Name:        t.Name,
		CreatedAt:   t.CreatedAt,
		ExpiresAt:   t.ExpiresAt,
	}); err != nil {
		return fmt.Errorf("store: creating operator token: %w", err)
	}
	return nil
}

// FindOperatorTokenByHash implements operator.Store.
func (s *Store) FindOperatorTokenByHash(ctx context.Context, hash []byte) (*operator.Token, error) {
	row, err := s.q.GetOperatorTokenByHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, operator.ErrTokenUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up operator token: %w", err)
	}
	t := operatorTokenFromRow(row)
	return &t, nil
}

// ListOperatorTokens implements operator.Store.
func (s *Store) ListOperatorTokens(ctx context.Context) ([]operator.Token, error) {
	rows, err := s.q.ListOperatorTokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing operator tokens: %w", err)
	}
	out := make([]operator.Token, 0, len(rows))
	for _, row := range rows {
		out = append(out, operatorTokenFromRow(row))
	}
	return out, nil
}

// RevokeOperatorToken implements operator.Store.
func (s *Store) RevokeOperatorToken(ctx context.Context, id uuid.UUID, now time.Time) error {
	n, err := s.q.RevokeOperatorToken(ctx, db.RevokeOperatorTokenParams{ID: id, RevokedAt: &now})
	if err != nil {
		return fmt.Errorf("store: revoking operator token: %w", err)
	}
	if n == 1 {
		return nil
	}
	if _, err := s.q.GetOperatorToken(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return operator.ErrTokenUnknown
	} else if err != nil {
		return fmt.Errorf("store: looking up operator token: %w", err)
	}
	return operator.ErrTokenRevoked
}

// RecordOperatorTokenUse implements operator.Store.
func (s *Store) RecordOperatorTokenUse(ctx context.Context, id uuid.UUID, now time.Time) error {
	if err := s.q.RecordOperatorTokenUse(ctx, db.RecordOperatorTokenUseParams{ID: id, LastUsedAt: &now}); err != nil {
		return fmt.Errorf("store: recording operator token use: %w", err)
	}
	return nil
}

func operatorTokenFromRow(row db.OperatorToken) operator.Token {
	return operator.Token{
		ID:         row.ID,
		Hash:       row.TokenHash,
		Prefix:     row.TokenPrefix,
		Name:       row.Name,
		CreatedAt:  row.CreatedAt,
		ExpiresAt:  row.ExpiresAt,
		RevokedAt:  row.RevokedAt,
		LastUsedAt: row.LastUsedAt,
	}
}
