# ADR-0004: Token-gated enrollment, URI SAN identity, embedded CA behind an interface

**Status:** Accepted

## Context

The only hard identity problem is the first certificate. Trust-on-first-use (accept whoever connects first, alert on change) has no trust story at registration: during the open window, anyone reachable can enroll as anything, receive policy, and learn topology — unacceptable for a system that enforces and whose policy discloses network structure. Long-lived baked-in secrets are worse. After first issuance, everything is routine mTLS rotation.

## Decision

- **Enrollment policies** define constraints (allowed labels, expiry, revocation); **join tokens** (provisioning tokens) are minted from them and passed to the installer. A token may enroll many workloads within its scope.
- Bootstrap: agent generates its keypair locally (private key never leaves the host) → CSR + token + host metadata over server-authenticated TLS → control plane validates, signs with short TTL, returns cert + chain → the token's use is recorded.
- Certificates carry exactly one **URI SAN**, `innerwall://workload/<uuid>`, with the UUID assigned by the control plane (form fixed by ADR-0016). Mutable attributes (hostname, labels) stay in the registry. **Cert = authentication; registry = authorization.**
- **Short TTLs** (24h by default), renewal in the last third of lifetime, jittered, by authenticated re-CSR. Revocation machinery is replaced by short TTLs plus a control-plane deny-list of agent IDs. Missed renewal ⇒ re-enrollment (manual by default, configurable).
- The CA is **embedded in the control plane for v1**, behind a `CertificateAuthority` interface so external issuers (secret-management or CA services, KMS/HSM-backed signing) can replace it without touching enrollment.
- Designed-for edge cases: clock skew (bounded notBefore backdating); cloned images presenting duplicate identities (detected on connect, forced re-enrollment).

## Consequences

- Enrollment trust anchors in an operator-generated, scoped, expiring secret — auditable and revocable pre-use.
- Short TTLs make agent renewal a hot path; the CA endpoint is availability-critical for renewals (fail-static covers enforcement regardless — ADR-0011).
- The interface commits us to keeping enrollment logic CA-agnostic.

## Amendments

- **2026-09-13 (PR #5).** ADR-0016 implements this record's identity semantics at the signing boundary (the exact URI form, the signing-request rule, the listener boundary, and the token as the carrier of enrollment-policy constraints); it does not supersede this record, and the earlier `Superseded by ADR-0016` status line is corrected.
- **2026-09-13 (PR #5): body corrected to ADR-0016's forms.** Three decision bullets stated details that the implementation fixed differently and that ADR-0016 records: the URI SAN is `innerwall://workload/<uuid>`, one per certificate, and the other project's naming that the original bullet and title borrowed is removed outright per the vocabulary rule; renewal happens in the last third of lifetime, jittered, not at half; tokens are reusable within their scope with a use recorded, not burned or decremented, and enrollment policy constraints are labels, expiry, and revocation. The core decision, token-gated enrollment with no trust-on-first-use window, a control-plane-assigned identity in a URI SAN, short-lived credentials, and a signing authority behind an interface, stands. Prior text is in git history.
