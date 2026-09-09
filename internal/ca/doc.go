// Package ca defines the CertificateAuthority interface and the embedded v1
// implementation. Certificates carry identity only: a SPIFFE-style URI SAN
// with a control-plane-assigned UUID. Mutable attributes never enter a
// certificate (ADR-0004).
package ca
