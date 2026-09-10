# ADR-0004: Token-gated enrollment, SPIFFE-style identity, embedded CA behind an interface

**Status:** Superseded by ADR-0016

## Context

The only hard identity problem is the first certificate. Trust-on-first-use (accept whoever connects first, alert on change) has no trust story at registration: during the open window, anyone reachable can enroll as anything, receive policy, and learn topology — unacceptable for a system that enforces and whose policy discloses network structure. Long-lived baked-in secrets are worse. After first issuance, everything is routine mTLS rotation.

## Decision

- **Enrollment policies** define constraints (allowed labels, expiry, one-time or N-use limits); **join tokens** are minted from them and passed to the installer.
- Bootstrap: agent generates its keypair locally (private key never leaves the host) → CSR + token + host metadata over server-authenticated TLS → control plane validates, signs with short TTL, returns cert + chain → token burned/decremented.
- Certificates carry a **SPIFFE-style URI SAN** (`spiffe://innerwall/agent/<uuid>`), UUID assigned by the control plane. Mutable attributes (hostname, labels) stay in the registry. **Cert = authentication; registry = authorization.**
- **24–48h TTLs**, renewal at ~50% lifetime by authenticated re-CSR. Revocation machinery is replaced by short TTLs plus a control-plane deny-list of agent IDs. Missed renewal ⇒ re-enrollment (manual by default, configurable).
- The CA is **embedded in the control plane for v1**, behind a `CertificateAuthority` interface so external issuers (secret-management or CA services, KMS/HSM-backed signing) can replace it without touching enrollment.
- Designed-for edge cases: clock skew (bounded notBefore backdating); cloned images presenting duplicate identities (detected on connect, forced re-enrollment).

## Consequences

- Enrollment trust anchors in an operator-generated, scoped, expiring secret — auditable and revocable pre-use.
- Short TTLs make agent renewal a hot path; the CA endpoint is availability-critical for renewals (fail-static covers enforcement regardless — ADR-0011).
- The interface commits us to keeping enrollment logic CA-agnostic.
