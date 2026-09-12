// Package sync maintains the agent's single persistent stream to the
// control plane and applies versioned desired-state updates in order
// (ADR-0002, ADR-0015). Every stream starts with Hello and receives a
// snapshot; deltas follow on the same stream. Each update is applied
// atomically through the enforce.PolicyStore and acknowledged; a failed
// apply leaves the last good policy installed and is acknowledged FAILED,
// after which the control plane sends a fresh snapshot. Reconnects use
// exponential backoff with full jitter. Persistence of the last applied
// policy to disk, which makes the agent fail static across restarts
// (ADR-0011), lands with enforcement behind the same store interface.
package sync
