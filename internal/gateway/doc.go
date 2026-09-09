// Package gateway terminates agent mTLS and holds one persistent bidirectional
// gRPC stream per agent (ADR-0002). Policy deltas go down; aggregated flows,
// heartbeats, and ruleset-version ACKs come up. A presence table in Postgres
// lets any stateless replica route a push to the replica holding the stream
// (ADR-0005). Reconnection storms are a first-class design case.
package gateway
