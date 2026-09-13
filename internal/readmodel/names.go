package readmodel

import (
	"fmt"
	"strings"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// The names the command line and the operator surface both speak for the
// wire contract's enumerations: the enumeration's name without its
// prefix, in lower case.

// ParseVerdict parses a decision name: observed, allowed, would_block, or
// blocked. Empty means every decision.
func ParseVerdict(s string) (innerwallv1.PolicyDecision, error) {
	if s == "" {
		return innerwallv1.PolicyDecision_POLICY_DECISION_UNSPECIFIED, nil
	}
	v, ok := innerwallv1.PolicyDecision_value["POLICY_DECISION_"+strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", "_"))]
	if !ok || v == 0 {
		return 0, fmt.Errorf("readmodel: unknown verdict %q (observed|allowed|would_block|blocked)", s)
	}
	return innerwallv1.PolicyDecision(v), nil
}

// VerdictName is the inverse of ParseVerdict.
func VerdictName(d innerwallv1.PolicyDecision) string {
	return strings.ToLower(strings.TrimPrefix(d.String(), "POLICY_DECISION_"))
}

// ParseSyncState parses a sync state name: synced, pending, degraded, or
// offline. Empty means every state.
func ParseSyncState(s string) (innerwallv1.SyncState, error) {
	if s == "" {
		return innerwallv1.SyncState_SYNC_STATE_UNSPECIFIED, nil
	}
	v, ok := innerwallv1.SyncState_value["SYNC_STATE_"+strings.ToUpper(strings.TrimSpace(s))]
	if !ok || v == 0 {
		return 0, fmt.Errorf("readmodel: unknown sync state %q (synced|pending|degraded|offline)", s)
	}
	return innerwallv1.SyncState(v), nil
}

// SyncStateName is the inverse of ParseSyncState.
func SyncStateName(s innerwallv1.SyncState) string {
	return strings.ToLower(strings.TrimPrefix(s.String(), "SYNC_STATE_"))
}
