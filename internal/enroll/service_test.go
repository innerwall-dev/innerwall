package enroll_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/enroll/enrolltest"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
)

func newCSR(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func newService(t *testing.T) (*enroll.Service, *enrolltest.MemStore) {
	t.Helper()
	authority, err := fileca.Init(t.TempDir(), fileca.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	st := enrolltest.NewMemStore()
	return &enroll.Service{Store: st, Authority: authority, LeafTTL: time.Hour}, st
}

func TestEnrollHappyPath(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	labels := []enroll.Label{{"env", "prod"}, {"role", "db"}}
	plain, tok, err := svc.MintToken(ctx, "prod-db", labels, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tok.ExpiresAt.Sub(tok.CreatedAt).Round(time.Second) != enroll.DefaultTokenTTL {
		t.Fatalf("default ttl not applied: %v", tok.ExpiresAt.Sub(tok.CreatedAt))
	}

	res, err := svc.Enroll(ctx, plain, newCSR(t), "db-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Workload.ID.IsZero() {
		t.Fatal("no workload id")
	}
	if !slices.Equal(res.Workload.Labels, labels) {
		t.Fatalf("labels = %v, want %v", res.Workload.Labels, labels)
	}
	if res.Workload.Hostname != "db-1" || res.Workload.TokenID != tok.ID {
		t.Fatalf("workload = %+v", res.Workload)
	}
	if res.Credential.Serial == "" || res.Credential.Serial != res.Workload.CredentialSerial {
		t.Fatalf("serial = %q / %q", res.Credential.Serial, res.Workload.CredentialSerial)
	}
	block, _ := pem.Decode(res.Credential.CertificatePEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := identity.FromCertificate(cert); got != res.Workload.ID {
		t.Fatalf("certificate identity %v != %v", got, res.Workload.ID)
	}
	stored, _ := st.GetWorkload(ctx, res.Workload.ID)
	if stored == nil || stored.CredentialSerial != res.Credential.Serial {
		t.Fatalf("workload not persisted: %+v", stored)
	}
	if st.Token(tok.ID).UseCount != 1 {
		t.Fatalf("use count = %d", st.Token(tok.ID).UseCount)
	}

	// Multi-use within scope: a second enrollment gets a distinct identity.
	res2, err := svc.Enroll(ctx, plain, newCSR(t), "db-2")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Workload.ID == res.Workload.ID {
		t.Fatal("two enrollments share an identity")
	}
}

func TestEnrollRejectsBadTokens(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	csr := newCSR(t)

	if _, err := svc.Enroll(ctx, "nonsense", csr, ""); !errors.Is(err, enroll.ErrTokenMalformed) {
		t.Fatalf("malformed: err = %v", err)
	}
	unknown, _, _ := enroll.NewToken()
	if _, err := svc.Enroll(ctx, unknown, csr, ""); !errors.Is(err, enroll.ErrTokenUnknown) {
		t.Fatalf("unknown: err = %v", err)
	}

	expired, _, err := svc.MintToken(ctx, "short", nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return time.Now().Add(time.Minute) }
	if _, err := svc.Enroll(ctx, expired, csr, ""); !errors.Is(err, enroll.ErrTokenExpired) {
		t.Fatalf("expired: err = %v", err)
	}
	svc.Now = nil

	revoked, tok, err := svc.MintToken(ctx, "revoked", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.RevokeToken(ctx, tok.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enroll(ctx, revoked, csr, ""); !errors.Is(err, enroll.ErrTokenRevoked) {
		t.Fatalf("revoked: err = %v", err)
	}
	if err := svc.Store.RevokeToken(ctx, tok.ID, time.Now()); !errors.Is(err, enroll.ErrTokenRevoked) {
		t.Fatalf("double revoke: err = %v", err)
	}
}

func TestEnrollRejectsBadCSRAfterToken(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	plain, tok, _ := svc.MintToken(ctx, "x", nil, 0)
	if _, err := svc.Enroll(ctx, plain, []byte("not a csr"), ""); err == nil {
		t.Fatal("bad csr accepted")
	}
	if st.Token(tok.ID).UseCount != 0 {
		t.Fatal("failed enrollment counted as a use")
	}
	if st.WorkloadCount() != 0 {
		t.Fatal("failed enrollment persisted a workload")
	}
}

func TestMintTokenValidatesLabels(t *testing.T) {
	svc, _ := newService(t)
	if _, _, err := svc.MintToken(context.Background(), "x", []enroll.Label{{"", "v"}}, 0); err == nil {
		t.Fatal("empty key accepted")
	}
	if _, _, err := svc.MintToken(context.Background(), "x", []enroll.Label{{"k", "1"}, {"k", "2"}}, 0); !errors.Is(err, policy.ErrDuplicateLabelKey) {
		t.Fatalf("duplicate key: err = %v", err)
	}
	// What a paste of two labels into one field produced: one value
	// holding a space and a second key=value (ADR-0022).
	if _, _, err := svc.MintToken(context.Background(), "x", []enroll.Label{{"app", "web env=lab"}}, 0); !errors.Is(err, policy.ErrBadLabelValue) || policy.AsFindings(err) == nil {
		t.Fatalf("value with whitespace and '=': err = %v", err)
	}
	if _, _, err := svc.MintToken(context.Background(), "x", []enroll.Label{{"app web", "x"}}, 0); !errors.Is(err, policy.ErrBadLabelKey) {
		t.Fatalf("key with whitespace: err = %v", err)
	}
	if _, _, err := svc.MintToken(context.Background(), "x", []enroll.Label{{"app", "web"}, {"env", "lab"}}, 0); err != nil {
		t.Fatal(err)
	}
}

// TestEnrollRefusesTokenWithLabelsOutsideGrammar holds a token minted
// before the grammar was admitted: enrolling with it would hand the
// workload a label no selector can match, so it is refused before a
// credential is issued, and nothing is persisted.
func TestEnrollRefusesTokenWithLabelsOutsideGrammar(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	plain, hash, err := enroll.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	tok := enroll.Token{ID: uuid.New(), Hash: hash, Name: "legacy", Labels: []enroll.Label{{"app", "web env=lab"}}, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.CreateToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enroll(ctx, plain, newCSR(t), "web-1"); !errors.Is(err, enroll.ErrTokenLabelsInvalid) || !errors.Is(err, policy.ErrBadLabelValue) {
		t.Fatalf("err = %v", err)
	}
	if st.WorkloadCount() != 0 || st.Token(tok.ID).UseCount != 0 {
		t.Fatal("refused enrollment persisted a workload or counted a use")
	}
}

func TestMintTokenKeepsListingHint(t *testing.T) {
	svc, _ := newService(t)
	plain, tok, err := svc.MintToken(context.Background(), "x", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Prefix == nil || *tok.Prefix != enroll.ListingHint(plain) {
		t.Fatalf("minted token prefix = %v, want %q", tok.Prefix, enroll.ListingHint(plain))
	}
	listed, err := svc.ListTokens(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Prefix == nil || *listed[0].Prefix != *tok.Prefix {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
}

func TestRenew(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	plain, _, _ := svc.MintToken(ctx, "x", []enroll.Label{{"role", "web"}}, 0)
	res, err := svc.Enroll(ctx, plain, newCSR(t), "web-1")
	if err != nil {
		t.Fatal(err)
	}

	presented := time.Now().Add(-time.Minute)
	cred, err := svc.Renew(ctx, res.Workload.ID, presented, res.Credential.ExpiresAt, newCSR(t))
	if err != nil {
		t.Fatal(err)
	}
	if cred.LateBy != 0 {
		t.Fatalf("on-time renewal reported late by %v", cred.LateBy)
	}
	if cred.Serial == res.Credential.Serial {
		t.Fatal("renewal reused the serial")
	}
	block, _ := pem.Decode(cred.CertificatePEM)
	cert, _ := x509.ParseCertificate(block.Bytes)
	if got, _ := identity.FromCertificate(cert); got != res.Workload.ID {
		t.Fatalf("renewed identity %v != %v", got, res.Workload.ID)
	}
	w, _ := st.GetWorkload(ctx, res.Workload.ID)
	if w.CredentialSerial != cred.Serial || w.LastRenewedAt == nil || w.LateRenewal != nil {
		t.Fatalf("renewal not recorded as on time: %+v", w)
	}

	stranger, _ := identity.NewWorkloadID()
	if _, err := svc.Renew(ctx, stranger, presented, res.Credential.ExpiresAt, newCSR(t)); !errors.Is(err, enroll.ErrWorkloadUnknown) {
		t.Fatalf("unknown workload: err = %v", err)
	}
}

// TestAdmitCredential is the validity rule per call: a valid credential
// may make any call; an expired one may only renew, and only within the
// grace; beyond it every call is refused with a reason naming
// re-enrollment; a credential not yet valid is refused outright.
func TestAdmitCredential(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	svc := &enroll.Service{Now: func() time.Time { return now }}
	if svc.Grace() != enroll.DefaultRenewalGrace || enroll.DefaultRenewalGrace != 72*time.Hour {
		t.Fatalf("grace = %v, want the 72h default", svc.Grace())
	}
	issued := func(expiredBy time.Duration) (time.Time, time.Time) {
		notAfter := now.Add(-expiredBy)
		return notAfter.Add(-24 * time.Hour), notAfter
	}
	for _, tc := range []struct {
		name      string
		expiredBy time.Duration
		renewal   bool
		want      error
		mentions  string
	}{
		{"valid, any call", -time.Hour, false, nil, ""},
		{"valid, renewal", -time.Hour, true, nil, ""},
		{"at expiry, any call", 0, false, nil, ""},
		{"expired 1h, renewal", time.Hour, true, nil, ""},
		{"expired 1h, other call", time.Hour, false, enroll.ErrCredentialExpired, "may only renew"},
		{"expired exactly the grace, renewal", 72 * time.Hour, true, nil, ""},
		{"expired grace+1h, renewal", 73 * time.Hour, true, enroll.ErrCredentialExpired, enroll.ReenrollRequired},
		{"expired grace+1h, other call", 73 * time.Hour, false, enroll.ErrCredentialExpired, enroll.ReenrollRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nb, na := issued(tc.expiredBy)
			err := svc.AdmitCredential(nb, na, tc.renewal)
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.mentions != "" && !strings.Contains(err.Error(), tc.mentions) {
				t.Fatalf("err = %q, want it to say %q", err, tc.mentions)
			}
		})
	}
	if err := svc.AdmitCredential(now.Add(time.Minute), now.Add(time.Hour), true); !errors.Is(err, enroll.ErrCredentialNotYetValid) {
		t.Fatalf("not yet valid: err = %v", err)
	}
	// The grace is configurable.
	short := &enroll.Service{Now: svc.Now, RenewalGrace: time.Hour}
	nb, na := issued(2 * time.Hour)
	if err := short.AdmitCredential(nb, na, true); !errors.Is(err, enroll.ErrCredentialExpired) {
		t.Fatalf("beyond a 1h grace: err = %v", err)
	}
}

// TestStandingAt is the classification admission applies and the
// operator surface reports: valid through the expiry instant, in grace
// through exactly the grace after it, past grace beyond.
func TestStandingAt(t *testing.T) {
	notAfter := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	grace := 72 * time.Hour
	for _, tc := range []struct {
		name string
		at   time.Duration
		want enroll.Standing
	}{
		{"before expiry", -time.Hour, enroll.StandingValid},
		{"at expiry", 0, enroll.StandingValid},
		{"just after expiry", time.Nanosecond, enroll.StandingInGrace},
		{"exactly the grace", grace, enroll.StandingInGrace},
		{"within a second past the grace", grace + 500*time.Millisecond, enroll.StandingInGrace},
		{"a second past the grace", grace + time.Second, enroll.StandingPastGrace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := enroll.StandingAt(notAfter, notAfter.Add(tc.at), grace); got != tc.want {
				t.Fatalf("standing = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRenewLate renews with a credential expired within the grace: a
// credential is issued, the renewal is recorded as late with the expiry
// it replaced, and a later on-time renewal leaves that record. Beyond the
// grace nothing is issued and nothing is recorded.
func TestRenewLate(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	plain, _, _ := svc.MintToken(ctx, "x", nil, 0)
	res, err := svc.Enroll(ctx, plain, newCSR(t), "web-1")
	if err != nil {
		t.Fatal(err)
	}
	expired := res.Credential.ExpiresAt
	issuedAt := expired.Add(-time.Hour)
	now := expired.Add(time.Hour)
	svc.Now = func() time.Time { return now }

	cred, err := svc.Renew(ctx, res.Workload.ID, issuedAt, expired, newCSR(t))
	if err != nil {
		t.Fatalf("renewal an hour after expiry: %v", err)
	}
	if cred.LateBy != time.Hour {
		t.Fatalf("LateBy = %v, want 1h", cred.LateBy)
	}
	w, _ := st.GetWorkload(ctx, res.Workload.ID)
	if w.LateRenewal == nil || !w.LateRenewal.RenewedAt.Equal(now) || !w.LateRenewal.ExpiredAt.Equal(expired) {
		t.Fatalf("late renewal recorded as %+v, want at %v for expiry %v", w.LateRenewal, now, expired)
	}

	// On time: the late record stands.
	if _, err := svc.Renew(ctx, res.Workload.ID, now.Add(-time.Minute), now.Add(time.Hour), newCSR(t)); err != nil {
		t.Fatal(err)
	}
	w, _ = st.GetWorkload(ctx, res.Workload.ID)
	if w.LateRenewal == nil || !w.LateRenewal.ExpiredAt.Equal(expired) {
		t.Fatalf("on-time renewal disturbed the late record: %+v", w.LateRenewal)
	}

	// Beyond the grace.
	serial := w.CredentialSerial
	now = expired.Add(svc.Grace() + time.Hour)
	if _, err := svc.Renew(ctx, res.Workload.ID, issuedAt, expired, newCSR(t)); !errors.Is(err, enroll.ErrCredentialExpired) || !strings.Contains(err.Error(), enroll.ReenrollRequired) {
		t.Fatalf("beyond the grace: err = %v", err)
	}
	if w, _ = st.GetWorkload(ctx, res.Workload.ID); w.CredentialSerial != serial {
		t.Fatal("a refused renewal was recorded")
	}
}
