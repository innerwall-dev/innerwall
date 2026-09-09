// Package ingest receives pre-aggregated flow windows from agents, enriches
// them (address → workload identity via the registry), deduplicates
// bidirectionally (both endpoints of a connection report it), and writes them
// through the FlowStore interface (ADR-0009).
package ingest
