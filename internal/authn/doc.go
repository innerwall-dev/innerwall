// Package authn is the authentication boundary of the operator surface
// (ADR-0021). It defines the one principal every caller of the domain
// layer acts as, and how that principal travels in a context to the
// domain-function boundary.
//
// Version 1 has one operator and binary authorization: a caller is
// authenticated or it is not. The principal therefore names the one
// operator and nothing more; it carries no record of which credential
// produced it, no role, and no scope.
package authn
