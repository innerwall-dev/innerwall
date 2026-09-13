// Package operatortest holds test doubles for the operator domain.
package operatortest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/operator"
)

// MemStore is an in-memory operator.Store for exercising the domain and the
// surface without a database. The Postgres implementation is tested in
// internal/store.
type MemStore struct {
	mu       sync.Mutex
	op       *operator.Operator
	sessions map[string]*operator.Session
	tokens   map[uuid.UUID]*operator.Token
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{sessions: map[string]*operator.Session{}, tokens: map[uuid.UUID]*operator.Token{}}
}

// GetOperator implements operator.Store.
func (m *MemStore) GetOperator(context.Context) (*operator.Operator, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.op == nil {
		return nil, operator.ErrNoOperator
	}
	cp := *m.op
	return &cp, nil
}

// SetOperator implements operator.Store.
func (m *MemStore) SetOperator(_ context.Context, op operator.Operator) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.op != nil {
		op.CreatedAt = m.op.CreatedAt
	}
	m.op = &op
	return nil
}

// CreateSession implements operator.Store.
func (m *MemStore) CreateSession(_ context.Context, s operator.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := s
	m.sessions[string(s.IDHash)] = &cp
	return nil
}

// GetSession implements operator.Store.
func (m *MemStore) GetSession(_ context.Context, idHash []byte) (*operator.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(idHash)]
	if !ok {
		return nil, operator.ErrSessionUnknown
	}
	cp := *s
	return &cp, nil
}

// DeleteSession implements operator.Store.
func (m *MemStore) DeleteSession(_ context.Context, idHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, string(idHash))
	return nil
}

// DeleteExpiredSessions implements operator.Store.
func (m *MemStore) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k, s := range m.sessions {
		if !s.ExpiresAt.After(now) {
			delete(m.sessions, k)
			n++
		}
	}
	return n, nil
}

// SessionCount reports how many sessions are stored.
func (m *MemStore) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// CreateOperatorToken implements operator.Store.
func (m *MemStore) CreateOperatorToken(_ context.Context, t operator.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := t
	m.tokens[t.ID] = &cp
	return nil
}

// FindOperatorTokenByHash implements operator.Store.
func (m *MemStore) FindOperatorTokenByHash(_ context.Context, hash []byte) (*operator.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if operator.HashEqual(t.Hash, hash) {
			cp := *t
			return &cp, nil
		}
	}
	return nil, operator.ErrTokenUnknown
}

// ListOperatorTokens implements operator.Store.
func (m *MemStore) ListOperatorTokens(context.Context) ([]operator.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]operator.Token, 0, len(m.tokens))
	for _, t := range m.tokens {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// RevokeOperatorToken implements operator.Store.
func (m *MemStore) RevokeOperatorToken(_ context.Context, id uuid.UUID, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[id]
	if !ok {
		return operator.ErrTokenUnknown
	}
	if t.RevokedAt != nil {
		return operator.ErrTokenRevoked
	}
	t.RevokedAt = &now
	return nil
}

// RecordOperatorTokenUse implements operator.Store.
func (m *MemStore) RecordOperatorTokenUse(_ context.Context, id uuid.UUID, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tokens[id]; ok {
		t.LastUsedAt = &now
	}
	return nil
}
