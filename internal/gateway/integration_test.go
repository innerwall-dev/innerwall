package gateway_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestEnrollmentEndToEnd runs the complete loop against Postgres and a real
// TLS listener: mint a token, enroll with the agent-side client, verify the
// issued credential against the authority bundle, renew over mutual TLS,
// revoke the token, and confirm enrollment then fails with the precise
// reason. It skips when no test database is configured.
func TestEnrollmentEndToEnd(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Mint.
	labels := []enroll.Label{{Key: "env", Value: "prod"}, {Key: "role", Value: "db"}}
	plain, tok, err := h.service.MintToken(ctx, "prod-db", labels, 0)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Enroll, exactly as the agent command does.
	stateDir := t.TempDir()
	store := credential.Store{Dir: stateDir}
	key, err := credential.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	res, err := credential.Enroll(ctx, key, credential.EnrollOptions{
		Server:      h.addr,
		Token:       plain,
		BootstrapCA: h.bundle,
		Facts:       &innerwallv1.HostFacts{Hostname: "db-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(key, res.CertificatePEM, res.BundlePEM); err != nil {
		t.Fatal(err)
	}

	// 3. The issued certificate verifies against Bundle() and carries the
	// expected SAN, and nothing else.
	bundle, err := h.authority.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cert := parsePEMCert(t, res.CertificatePEM)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(bundle)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("issued certificate does not verify against Bundle(): %v", err)
	}
	if got, err := identity.FromCertificate(cert); err != nil || got != res.WorkloadID {
		t.Fatalf("identity = %v, %v; want %v", got, err, res.WorkloadID)
	}
	if len(cert.URIs) != 1 || cert.URIs[0].String() != "innerwall://workload/"+res.WorkloadID.String() {
		t.Fatalf("URIs = %v", cert.URIs)
	}
	if len(cert.DNSNames)+len(cert.IPAddresses)+len(cert.EmailAddresses) != 0 {
		t.Fatalf("unexpected SANs: %v %v %v", cert.DNSNames, cert.IPAddresses, cert.EmailAddresses)
	}
	if len(res.Labels) != 2 || res.Labels[0].GetKey() != "env" || res.Labels[1].GetValue() != "db" {
		t.Fatalf("assigned labels = %v", res.Labels)
	}

	// 4. The workload row is persisted with the token's labels.
	w, err := st.GetWorkload(ctx, res.WorkloadID)
	if err != nil {
		t.Fatal(err)
	}
	if w.TokenID != tok.ID || w.Hostname != "db-1" || w.CredentialSerial != cert.SerialNumber.Text(16) {
		t.Fatalf("workload = %+v", w)
	}
	if len(w.Labels) != 2 || w.Labels[0] != labels[0] || w.Labels[1] != labels[1] {
		t.Fatalf("stored labels = %v, want %v", w.Labels, labels)
	}

	// 5. Renew over mTLS with the stored credential: new serial, same SAN.
	cred, trust, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	storedKey, err := store.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := credential.Renew(ctx, storedKey, credential.RenewOptions{Server: h.addr, Credential: cred, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceCertificate(renewed.CertificatePEM, renewed.BundlePEM); err != nil {
		t.Fatal(err)
	}
	newCert := parsePEMCert(t, renewed.CertificatePEM)
	if newCert.SerialNumber.Cmp(cert.SerialNumber) == 0 {
		t.Fatal("renewal reused the serial")
	}
	if got, _ := identity.FromCertificate(newCert); got != res.WorkloadID {
		t.Fatalf("renewed identity = %v, want %v", got, res.WorkloadID)
	}
	if _, err := newCert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("renewed certificate does not verify: %v", err)
	}
	w, _ = st.GetWorkload(ctx, res.WorkloadID)
	if w.CredentialSerial != newCert.SerialNumber.Text(16) || w.LastRenewedAt == nil {
		t.Fatalf("renewal not recorded: %+v", w)
	}
	// The renewed credential works for the next renewal too.
	cred2, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credential.Renew(ctx, storedKey, credential.RenewOptions{Server: h.addr, Credential: cred2, Trust: trust}); err != nil {
		t.Fatalf("renewal with renewed credential: %v", err)
	}

	// 6. Renewal without a credential is refused; the token is not a
	// substitute on the agent side of the boundary.
	if _, err := credential.Renew(ctx, storedKey, credential.RenewOptions{Server: h.addr, Credential: tls.Certificate{}, Trust: trust}); err == nil {
		t.Fatal("renewal without a credential succeeded")
	}

	// 7. Revoke, then enrollment fails with the precise reason.
	if err := st.RevokeToken(ctx, tok.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	key2, _ := credential.GenerateKey()
	_, err = credential.Enroll(ctx, key2, credential.EnrollOptions{Server: h.addr, Token: plain, BootstrapCA: h.bundle})
	if err == nil {
		t.Fatal("enrollment with a revoked token succeeded")
	}
	// status.FromError sees through the client wrapper but reports the
	// outer message, so match the reason as a substring.
	s, ok := status.FromError(err)
	if !ok || s.Code() != codes.Unauthenticated || !strings.Contains(s.Message(), enroll.ErrTokenRevoked.Error()) {
		t.Fatalf("revoked enrollment err = %v", err)
	}
	// The already-enrolled workload keeps renewing: revoking a token
	// affects future enrollments, not identities it already granted.
	cred3, _, _ := store.Load()
	if _, err := credential.Renew(ctx, storedKey, credential.RenewOptions{Server: h.addr, Credential: cred3, Trust: trust}); err != nil {
		t.Fatalf("renewal after token revocation: %v", err)
	}
}

func parsePEMCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
