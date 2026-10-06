// Package ingest receives pre-aggregated flow windows from agents,
// resolves each record's peer at ingest (address → workload identity via
// the registry), and writes the windows and their evidence gaps through
// the FlowStore interface (ADR-0009, ADR-0019). It does not deduplicate:
// v1 observes inbound connections only (ADR-0010), so each connection is
// reported once, by the workload it reached, and stored as reported.
package ingest
