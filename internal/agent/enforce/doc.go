// Package enforce holds the agent's installed-policy boundary. PolicyStore
// is what the sync loop installs each complete rendered policy into,
// atomically: the in-memory implementation here is the whole of it until
// the nftables implementation lands behind the same interface, replacing
// the owned table as a unit and never touching state outside it (ADR-0003).
// The local kill switch drops the owned table without control-plane
// involvement (ADR-0011).
package enforce
