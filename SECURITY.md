# Security Policy

## Reporting a vulnerability

Email **security@innerwall.dev**. Please include reproduction steps, affected component (agent, control plane, UI, enrollment), and impact as you understand it.

- Acknowledgment within 72 hours; a triage verdict and remediation plan target within 14 days.
- Please practice coordinated disclosure: give us a chance to ship a fix before publishing. We'll credit reporters in release notes unless you prefer otherwise.
- No public GitHub issues for vulnerabilities until a fix is released.

Supported versions: the latest minor release. Pre-1.0, fixes land on `main` and ship in the next release.

## Trust model (summary)

The full model lives in `ARCHITECTURE.md` §11 and ADR-0016/0011; the load-bearing properties:

- **The agent is root-privileged by necessity** (it programs the host firewall) and minimized by design: static binary, no listening ports (outbound-only dialing), no runtime downloads, **no self-update**.
- **Fail static.** Control-plane loss never changes enforcement on any host; last-known policy persists locally.
- **Host operators outrank the platform.** A documented local kill switch disables enforcement without control-plane involvement. Root on the box is inside the trust boundary — we state this rather than pretend otherwise.
- **Enrollment is token-gated** (scoped, expiring, revocable provisioning tokens, stored only as hashes); workload identity is short-lived mTLS with a control-plane-assigned UUID in a URI SAN. A signing request contributes only its public key. There is no trust-on-first-use window.
- **The control plane is the high-value target**: policy changes are versioned and audited; a deny-list provides immediate agent lockout.
- **Supply chain:** reproducible static builds, checksummed and signed releases, DCO-enforced commit provenance.

Reports that assume root compromise of the host the agent runs on, or that require the local kill switch to be "bypassed" by root, are outside the model — that access level is explicitly inside the trust boundary.
