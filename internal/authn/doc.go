// Package authn is the authentication boundary of the operator surface
// (ADR-0021). It defines the one principal every caller of the domain
// layer acts as, how that principal travels in a context to the
// domain-function boundary, and the Authenticator interface the surface
// authenticates through: one verifier per credential form (the password,
// an operator token, a session) over one session minter.
//
// The implementation over the operator domain is operator.NewAuthenticator.
// Nothing outside internal/operator depends on how a credential is stored
// or checked.
//
// Version 1 has one operator and binary authorization: a caller is
// authenticated or it is not. The principal therefore names the one
// operator and nothing more; it carries no record of which credential
// produced it, no role, and no scope.
package authn
