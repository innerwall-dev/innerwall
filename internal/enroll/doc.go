// Package enroll implements enrollment policies and join tokens. A join token
// is minted from an enrollment policy and is the only credential an installer
// needs; the agent's private key never leaves its host, and the token is burned
// or decremented on use. There is no trust-on-first-use window (ADR-0004).
package enroll
