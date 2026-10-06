// Package enforce holds the agent's installed-policy boundary. PolicyStore
// is what the sync loop installs each complete rendered policy into,
// atomically, so a host runs one version or the next and never a mixture
// (ADR-0003). The production implementation is the nftables backend in
// enforce/nft, which persists each policy durably here (PolicyFile,
// WriteDurable) before it replaces the owned table as a unit, and never
// touches state outside that table (ADR-0020). MemoryStore is the
// in-memory implementation the sync loop's tests run against. The local
// kill switch drops the owned table without control-plane involvement
// (ADR-0011).
package enforce
