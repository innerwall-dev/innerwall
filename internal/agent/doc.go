// Package agent holds the agent internals as independent pieces sharing
// local state (ADR-0011): the sync daemon (sync/), which holds the stream,
// sends heartbeats and inventory, and applies each policy synchronously
// through the installed-policy store (enforce/); the credential holder and
// its renewal timer (credential/); and the flow collector and reporter
// (collect/). Keeping the pieces independent keeps the safety properties
// independently testable.
package agent
