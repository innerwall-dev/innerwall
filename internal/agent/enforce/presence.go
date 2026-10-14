package enforce

import (
	"context"
	"log/slog"
	"sync"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// probeTimeout bounds one read of the kernel's tables, so a heartbeat is
// never held up by a firewall tool that hangs.
const probeTimeout = 5 * time.Second

// Presence tracks whether the owned table the agent installed is still in
// the kernel. The local kill switch deletes it without the daemon or the
// control plane (ADR-0011), and with it goes everything the agent enforces
// and observes; the version the agent last acknowledged is unchanged by
// that, so it cannot say whether the table is there. Check reads the
// kernel when a heartbeat is composed and says, and the first check that
// finds the table gone reports the interval since it was last known
// present, so the blind period is recorded rather than read as quiet
// (issue #64).
//
// Presence never reinstalls the table: removing it is the operator's kill
// switch, and the daemon reinstalls only on its next apply or restart, as
// the kill switch says.
type Presence struct {
	// Probe reads whether the owned table is in the kernel.
	Probe func(ctx context.Context) (bool, error)
	// Installed reports whether the agent has installed a policy; before
	// one there is no table to expect, and Check says nothing. Always
	// true when nil.
	Installed func() bool
	// Removed is called when a check first finds the table gone, with the
	// earliest instant the removal can have happened: the last instant the
	// table was known present, or the check itself when none is known.
	Removed func(since time.Time)
	// Restored is called when an apply or a check finds the table
	// installed again after Removed.
	Restored func(at time.Time)
	Log      *slog.Logger
	// Now is the clock; time.Now when nil.
	Now func() time.Time

	mu     sync.Mutex
	known  time.Time
	absent bool
}

func (p *Presence) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Presence) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

// Check reads the kernel and returns the owned table's state as a
// heartbeat carries it: unspecified before any policy is installed or when
// the read fails, present, or absent. A nil Presence says nothing.
func (p *Presence) Check(ctx context.Context) innerwallv1.OwnedTableState {
	if p == nil || p.Probe == nil {
		return innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED
	}
	if p.Installed != nil && !p.Installed() {
		return innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	present, err := p.Probe(ctx)
	if err != nil {
		p.log().Warn("could not read whether the owned table is installed; the heartbeat reports it unknown", "error", err)
		return innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if present {
		p.restoredLocked(now)
		return innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT
	}
	if !p.absent {
		p.absent = true
		since := p.known
		if since.IsZero() {
			since = now
		}
		p.log().Error("THE OWNED TABLE IS GONE FROM THE KERNEL: nothing on this host is enforced or observed by Innerwall; the next policy update or daemon restart reinstalls it",
			"known_present_at", since)
		if p.Removed != nil {
			p.Removed(since)
		}
	}
	return innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT
}

// MarkInstalled records that an apply installed the table at at. A nil
// Presence records nothing.
func (p *Presence) MarkInstalled(at time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restoredLocked(at)
}

// restoredLocked records the table present at at and ends a removal. The
// caller holds the lock.
func (p *Presence) restoredLocked(at time.Time) {
	p.known = at
	if !p.absent {
		return
	}
	p.absent = false
	p.log().Info("the owned table is installed again", "at", at)
	if p.Restored != nil {
		p.Restored(at)
	}
}

// Watched is a PolicyStore whose every successful apply is reported to a
// Presence: an apply that succeeded installed the table, whatever the
// kernel held before it.
type Watched struct {
	PolicyStore
	Presence *Presence
}

// Apply implements PolicyStore.
func (w Watched) Apply(ctx context.Context, policy *innerwallv1.WorkloadPolicy) error {
	if err := w.PolicyStore.Apply(ctx, policy); err != nil {
		return err
	}
	if w.Presence != nil {
		w.Presence.MarkInstalled(w.Presence.now())
	}
	return nil
}
