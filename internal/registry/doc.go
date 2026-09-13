// Package registry is the inventory of workloads: their labels, enforcement
// mode, reported host facts and the addresses derived from them, listening
// services, agent details, and the convergence state the sync stream
// reports. The certificate authenticates; the registry authorizes
// (ADR-0016). Labels are the only input to policy; addresses are an input
// to rendering because peers resolve to them (ADR-0018).
package registry
