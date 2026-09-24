package fleettest

import (
	"context"
	"sync"

	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

var _ fleet.Directives = (*Directives)(nil)

// Directives records the reconnects directed through it in order, in
// place of the notification bridge. Err, when set, is returned instead.
type Directives struct {
	mu         sync.Mutex
	reconnects []identity.WorkloadID
	Err        error
}

// DirectReconnect implements fleet.Directives.
func (d *Directives) DirectReconnect(_ context.Context, id identity.WorkloadID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Err != nil {
		return d.Err
	}
	d.reconnects = append(d.reconnects, id)
	return nil
}

// Reconnects returns the workloads directed to reconnect so far.
func (d *Directives) Reconnects() []identity.WorkloadID {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]identity.WorkloadID(nil), d.reconnects...)
}
