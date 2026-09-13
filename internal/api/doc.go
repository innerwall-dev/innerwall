// Package api serves the operator surface: the public REST/JSON façade
// consumed by the console and by automation, on a listener of its own
// (ADR-0021). The façade is generated from the protobuf definitions in
// proto/ and the HTTP bindings beside them; nothing exists here that is not
// defined there first (ADR-0007). Design rules for estate-scale automation:
// bulk endpoints, cursor pagination everywhere, async jobs for anything
// that fans out.
//
// The package owns the listener's transport rules: TLS only; a middleware
// that resolves a session cookie or a bearer operator token to the one
// operator principal; an origin guard on every request that is not a read;
// a fixed-window throttle on the login endpoint; problem documents for
// every error. Handlers see a principal and nothing about how it was
// authenticated.
package api
