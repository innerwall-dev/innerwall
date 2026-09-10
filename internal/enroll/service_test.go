package enroll_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/enroll/enrolltest"
	"github.com/innerwall-dev/innerwall/internal/identity"
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
	if len(res.Workload.Labels) != 2 || res.Workload.Labels[0] != labels[0] || res.Workload.Labels[1] != labels[1] {
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
	if _, _, err := svc.MintToken(context.Background(), "x", []enroll.Label{{"k", "1"}, {"k", "2"}}, 0); err == nil {
		t.Fatal("duplicate key accepted")
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

	cred, err := svc.Renew(ctx, res.Workload.ID, newCSR(t))
	if err != nil {
		t.Fatal(err)
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
	if w.CredentialSerial != cred.Serial || w.LastRenewedAt == nil {
		t.Fatalf("renewal not recorded: %+v", w)
	}

	stranger, _ := identity.NewWorkloadID()
	if _, err := svc.Renew(ctx, stranger, newCSR(t)); !errors.Is(err, enroll.ErrWorkloadUnknown) {
		t.Fatalf("unknown workload: err = %v", err)
	}
}
