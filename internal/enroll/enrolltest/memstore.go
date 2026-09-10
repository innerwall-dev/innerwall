// Package enrolltest holds test doubles for the enrollment domain.
package enrolltest

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// MemStore is an in-memory enroll.Store for exercising enrollment without a
// database. The Postgres implementation is tested in internal/store.
type MemStore struct {
	mu        sync.Mutex
	tokens    map[uuid.UUID]*enroll.Token
	workloads map[identity.WorkloadID]*enroll.Workload
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{tokens: map[uuid.UUID]*enroll.Token{}, workloads: map[identity.WorkloadID]*enroll.Workload{}}
}

// CreateToken implements enroll.Store.
func (m *MemStore) CreateToken(_ context.Context, t enroll.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := t
	m.tokens[t.ID] = &cp
	return nil
}

// FindTokenByHash implements enroll.Store.
func (m *MemStore) FindTokenByHash(_ context.Context, hash []byte) (*enroll.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if enroll.HashEqual(t.Hash, hash) {
			cp := *t
			return &cp, nil
		}
	}
	return nil, enroll.ErrTokenUnknown
}

// ListTokens implements enroll.Store.
func (m *MemStore) ListTokens(context.Context) ([]enroll.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]enroll.Token, 0, len(m.tokens))
	for _, t := range m.tokens {
		out = append(out, *t)
	}
	return out, nil
}

// RevokeToken implements enroll.Store.
func (m *MemStore) RevokeToken(_ context.Context, id uuid.UUID, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[id]
	if !ok {
		return enroll.ErrTokenUnknown
	}
	if t.RevokedAt != nil {
		return enroll.ErrTokenRevoked
	}
	t.RevokedAt = &now
	return nil
}

// CreateWorkload implements enroll.Store.
func (m *MemStore) CreateWorkload(_ context.Context, w enroll.Workload, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := w
	m.workloads[w.ID] = &cp
	t := m.tokens[w.TokenID]
	t.UseCount++
	t.LastUsedAt = &now
	return nil
}

// GetWorkload implements enroll.Store.
func (m *MemStore) GetWorkload(_ context.Context, id identity.WorkloadID) (*enroll.Workload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workloads[id]
	if !ok {
		return nil, enroll.ErrWorkloadUnknown
	}
	cp := *w
	return &cp, nil
}

// RecordRenewal implements enroll.Store.
func (m *MemStore) RecordRenewal(_ context.Context, id identity.WorkloadID, serial string, expiresAt, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workloads[id]
	if !ok {
		return enroll.ErrWorkloadUnknown
	}
	w.CredentialSerial = serial
	w.CredentialExpiresAt = expiresAt
	w.LastRenewedAt = &now
	return nil
}

// Token returns a copy of the stored token, or nil.
func (m *MemStore) Token(id uuid.UUID) *enroll.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// WorkloadCount returns the number of stored workloads.
func (m *MemStore) WorkloadCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.workloads)
}
