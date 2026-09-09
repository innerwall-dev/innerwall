// Package collect defines the Collector interface and its conntrack
// implementation. Flows are aggregated in memory into
// (src, dst, port, proto, count, bytes) windows before shipping; per-connection
// records never leave the host (ADR-0009). An eBPF-based collector is a later,
// additive backend behind the same interface (ADR-0003).
package collect
