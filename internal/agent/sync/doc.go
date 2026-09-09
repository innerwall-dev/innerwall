// Package sync maintains the agent's single persistent stream to the control
// plane, applies versioned desired-state deltas, persists the last-ACKed
// ruleset to disk, and ACKs versions. Reconnects use exponential backoff with
// full jitter (ADR-0002). Persistence is what makes the agent fail static
// across restarts and reboots (ADR-0011).
package sync
