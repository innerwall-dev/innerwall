// Package policy is the authored policy domain: services, address groups,
// and rulesets as operators write them, the admission rules every mutation
// passes before it is persisted, and the persistence interface the store
// implements (ADR-0018).
//
// The authored model mirrors the authored layer of the wire contract
// (ADR-0015) with one addition the contract does not need on the wire: a
// rule may permit services by reference to a named service definition as
// well as inline, and the renderer expands the reference. Admission is
// where v1's inbound-only scope is enforced (ADR-0010): an outbound rule is
// rejected here with a precise error, and nothing downstream ever sees one.
package policy
