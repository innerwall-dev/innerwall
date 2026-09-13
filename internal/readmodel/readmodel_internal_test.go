package readmodel

import (
	"errors"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

func TestCursorsRoundTripAndRefuse(t *testing.T) {
	id, _ := identity.NewWorkloadID()
	at := time.Date(2026, 9, 13, 11, 0, 0, 123456000, time.UTC)

	fc := encodeFlowsCursor(flowstore.WindowCursor{WindowStart: at, ID: 42})
	got, err := decodeFlowsCursor(fc)
	if err != nil || !got.WindowStart.Equal(at) || got.ID != 42 {
		t.Fatalf("flows cursor round trip = %+v, %v", got, err)
	}
	wc := encodeWorkloadCursor(WorkloadCursor{SyncRank: 2, SeenKey: at, ID: id})
	w, err := decodeWorkloadCursor(wc)
	if err != nil || w.SyncRank != 2 || !w.SeenKey.Equal(at) || w.ID != id {
		t.Fatalf("workload cursor round trip = %+v, %v", w, err)
	}
	// A cursor from the other read, garbage, and a truncated token are
	// all refused as invalid rather than misread.
	for _, bad := range []string{wc, "not base64!", "", fc[:len(fc)-3], encodeCursor("f1", "x", "1"), encodeCursor("f1", "1")} {
		if bad == "" {
			continue
		}
		if _, err := decodeFlowsCursor(bad); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("flows cursor %q err = %v", bad, err)
		}
	}
	for _, bad := range []string{fc, "garbage", encodeCursor("w1", "1", "2", "not-a-uuid")} {
		if _, err := decodeWorkloadCursor(bad); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("workload cursor %q err = %v", bad, err)
		}
	}
	if c, err := decodeFlowsCursor(""); c != nil || err != nil {
		t.Fatal("empty cursor is the first page")
	}
}

func TestCredentialState(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	renewed := now.Add(-time.Hour)
	cases := []struct {
		name    string
		expires time.Time
		err     string
		want    CredentialState
	}{
		{"valid and quiet", now.Add(time.Hour), "", CredentialRenews},
		{"valid but failing", now.Add(time.Hour), "authority unreachable", CredentialRenewalFailed},
		{"expired", now.Add(-time.Second), "", CredentialExpired},
		{"expired and failing", now, "authority unreachable", CredentialExpired},
	}
	for _, tc := range cases {
		rec := &WorkloadRecord{LastRenewedAt: &renewed}
		rec.CredentialExpiresAt, rec.CredentialRenewalError = tc.expires, tc.err
		st := credentialStatus(rec, now)
		if st.State != tc.want || st.LastError != tc.err || st.LastRenewedAt != &renewed || !st.ExpiresAt.Equal(tc.expires) {
			t.Fatalf("%s: %+v", tc.name, st)
		}
	}
}

func TestTerminalVerdict(t *testing.T) {
	if TerminalVerdict(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY) != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED ||
		TerminalVerdict(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION) != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK ||
		TerminalVerdict(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED) != innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED ||
		TerminalVerdict(innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED) != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED {
		t.Fatal("terminal verdicts do not follow the modes")
	}
}

func TestParseService(t *testing.T) {
	ok := map[string]Service{
		"tcp/5432": {innerwallv1.Protocol_PROTOCOL_TCP, 5432},
		"UDP/53":   {innerwallv1.Protocol_PROTOCOL_UDP, 53},
		"icmp":     {innerwallv1.Protocol_PROTOCOL_ICMP, 0},
		"icmp/0":   {innerwallv1.Protocol_PROTOCOL_ICMP, 0},
	}
	for in, want := range ok {
		got, err := ParseService(in)
		if err != nil || got != want {
			t.Fatalf("ParseService(%q) = %+v, %v", in, got, err)
		}
		if back, _ := ParseService(got.String()); back != got {
			t.Fatalf("String round trip of %q = %q", in, got.String())
		}
	}
	for _, bad := range []string{"", "tcp", "tcp/", "tcp/70000", "sctp/1", "icmp/8", "tcp/x", "/5"} {
		if _, err := ParseService(bad); !errors.Is(err, ErrInvalidService) {
			t.Fatalf("ParseService(%q) err = %v", bad, err)
		}
	}
}

func TestNames(t *testing.T) {
	for _, s := range []string{"observed", "allowed", "would_block", "would-block", "BLOCKED"} {
		if _, err := ParseVerdict(s); err != nil {
			t.Fatalf("verdict %q: %v", s, err)
		}
	}
	if _, err := ParseVerdict("unspecified"); err == nil {
		t.Fatal("the zero verdict is not a name")
	}
	if v, _ := ParseVerdict(""); v != 0 {
		t.Fatal("empty verdict means any")
	}
	if VerdictName(innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK) != "would_block" {
		t.Fatal("verdict name")
	}
	if s, err := ParseSyncState("degraded"); err != nil || s != innerwallv1.SyncState_SYNC_STATE_DEGRADED || SyncStateName(s) != "degraded" {
		t.Fatal("sync state name")
	}
	if _, err := ParseSyncState("asleep"); err == nil {
		t.Fatal("unknown sync state accepted")
	}
}
