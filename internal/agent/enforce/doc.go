// Package enforce defines the Enforcer interface and the nftables
// implementation. All rules live in one Innerwall-owned table, replaced
// atomically as a unit; state outside that table is never touched (ADR-0003).
// The local kill switch drops the owned table without control-plane
// involvement (ADR-0011).
package enforce
