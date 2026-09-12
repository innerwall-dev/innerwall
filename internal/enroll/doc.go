// Package enroll is the enrollment domain: provisioning tokens and the
// exchange of a token plus a certificate signing request for a workload
// identity and credential, and the renewal of that credential by an already
// identified workload (ADR-0020).
//
// A provisioning token is the only credential an installer needs. It is
// scoped to a label set, expires, is revocable, and may enroll many
// workloads within its scope. The control plane stores only its hash. The
// agent's private key never leaves its host; there is no trust-on-first-use
// window (ADR-0004).
//
// Signing goes through ca.Authority; persistence goes through Store. This
// package knows nothing about how either is implemented.
package enroll
