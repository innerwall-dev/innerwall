// Package ca is the signing boundary for workload credentials. It defines the
// Authority interface that enrollment and renewal sign through, and the two
// rules every implementation shares: a certificate signing request contributes
// only its public key, and identity is granted by the control plane, never
// requested by the enrollee (ADR-0020). Certificates carry identity only: one
// URI SAN with a control-plane-assigned UUID. Mutable attributes never enter a
// certificate.
//
// The file-backed implementation lives in the fileca subpackage. Nothing
// outside that subpackage depends on how it stores its key.
package ca
