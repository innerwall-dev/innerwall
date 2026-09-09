// Package agent holds the agent internals as four independent loops sharing
// local state: sync, collect, enforce (reconcile), and health (ADR-0011).
// Keeping the loops independent keeps the safety properties independently
// testable.
package agent
