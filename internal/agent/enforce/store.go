package enforce

import (
	"context"
	"sync"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// PolicyStore is the installed desired state of one host. The sync loop
// computes each complete next policy (from a snapshot, or by applying a
// delta to the current policy) and calls Apply with it; it never asks the
// store to mutate rules incrementally. That is what lets an implementation
// replace the host's owned firewall table as a unit: a host runs version N
// or version N+1, never a mixture (ADR-0003, ADR-0015).
type PolicyStore interface {
	// Apply installs policy as the complete desired state. On success it is
	// the installed state; on error the previously installed state is
	// untouched, so the caller can acknowledge FAILED and stay on its last
	// good version.
	Apply(ctx context.Context, policy *innerwallv1.WorkloadPolicy) error
	// Current returns the installed policy, or nil before the first Apply.
	Current() *innerwallv1.WorkloadPolicy
}

// MemoryStore keeps the installed policy in memory. It is the store the
// daemon runs with until enforcement lands; it makes the apply loop, the
// acknowledgement protocol, and the failure path testable and observable
// without a firewall.
type MemoryStore struct {
	mu      sync.RWMutex
	current *innerwallv1.WorkloadPolicy
	// Fail, when set, is consulted before an apply; a non-nil error is
	// returned and nothing changes. Tests use it to exercise the FAILED
	// path; production leaves it nil.
	Fail func(policy *innerwallv1.WorkloadPolicy) error
}

var _ PolicyStore = (*MemoryStore)(nil)

// Apply implements PolicyStore.
func (m *MemoryStore) Apply(_ context.Context, policy *innerwallv1.WorkloadPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		if err := m.Fail(policy); err != nil {
			return err
		}
	}
	m.current = rendered.Canonical(policy)
	return nil
}

// Current implements PolicyStore.
func (m *MemoryStore) Current() *innerwallv1.WorkloadPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}
