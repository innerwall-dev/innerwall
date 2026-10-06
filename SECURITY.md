# Security Policy

## Reporting a vulnerability

Email **security@innerwall.dev**. Please include reproduction steps, affected component (agent, control plane, UI, enrollment), and impact as you understand it.

- Acknowledgment within 72 hours; a triage verdict and remediation plan target within 14 days.
- Please practice coordinated disclosure: give us a chance to ship a fix before publishing. We'll credit reporters in release notes unless you prefer otherwise.
- No public GitHub issues for vulnerabilities until a fix is released.

Supported versions: the latest minor release. Pre-1.0, fixes land on `main` and ship in the next release.

## Trust model (summary)

The full model lives in `ARCHITECTURE.md` §11 and ADR-0016/0011/0021; the load-bearing properties, stated as implemented:

- **The agent is root-privileged by necessity** (it programs the host firewall) and minimized by design: static binary, no listening ports (outbound-only dialing), no runtime downloads, **no self-update**.
- **Fail static.** Control-plane loss never changes enforcement on any host; last-known policy persists locally.
- **Host operators outrank the platform.** A documented local kill switch disables enforcement without control-plane involvement. Root on the box is inside the trust boundary — we state this rather than pretend otherwise.
- **Enrollment is token-gated** (scoped, expiring, revocable provisioning tokens, stored only as hashes); workload identity is short-lived mTLS with a control-plane-assigned UUID in a URI SAN. A signing request contributes only its public key. There is no trust-on-first-use window.
- **The operator surface is one TLS-only listener** (ADR-0021): a single operator whose password is set only from the command line on the host and stored as an argon2id hash; a session cookie that is `HttpOnly`, `Secure`, and `SameSite=Lax`; operator tokens stored only as digests; every request that is not a read refused when a browser marks it cross-origin, which is the cross-site write defense; and login attempts throttled per source address in process, in a table bounded at 4,096 sources.
- **The control plane is the high-value target**: every authored policy object is versioned and every write is conditioned on the version its author read; enforcement-mode changes are recorded as intents. Revocation in this version is short-lived credentials (24 hours by default) and token revocation: a revoked provisioning token enrolls nothing further, and an enrolled workload is locked out when its credential lapses. Immediate lockout of an enrolled workload (a deny-list of workload identities) and detection of one identity presented by two hosts are not implemented; both are named controls for a later version.
- **Supply chain:** static builds (CGO disabled, trimmed, timestamped from their commit); the release configuration checksums every archive and signs the checksum file; DCO-enforced provenance on every authored commit. Third-party code bundled in the console ships with its license texts.

Reports that assume root compromise of the host the agent runs on, or that require the local kill switch to be "bypassed" by root, are outside the model — that access level is explicitly inside the trust boundary.
