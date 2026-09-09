// Package health carries heartbeats on the stream, enforces the agent's own
// memory and buffer-disk budgets (degrading telemetry before ever degrading
// the host), and handles the local kill switch (ADR-0011).
package health
