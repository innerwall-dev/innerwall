package gateway_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// issueAt signs a workload credential for key and id with the harness's
// own root, valid for a day and expiring at notAfter: a credential the
// authority really issued, as of a time the test chooses, so expiry can be
// exercised without moving any clock.
func (h *harness) issueAt(t *testing.T, key *ecdsa.PrivateKey, id identity.WorkloadID, notAfter time.Time) tls.Certificate {
	t.Helper()
	if h.authorityDir == "" {
		t.Fatal("harness did not create its authority")
	}
	keyPEM, err := os.ReadFile(filepath.Join(h.authorityDir, fileca.KeyFile))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("root key holds no PEM block")
		return tls.Certificate{}
	}
	var rootKey any
	if rootKey, err = x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		if rootKey, err = x509.ParseECPrivateKey(block.Bytes); err != nil {
			t.Fatal(err)
		}
	}
	root := parsePEMCert(t, h.bundle)
	const ttl = 24 * time.Hour
	tmpl, err := ca.LeafTemplate(id, notAfter.Add(-ttl), ttl)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, root, &key.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	ownKey, _ := pemKey(key)
	cred, err := tls.X509KeyPair(ca.EncodeCertificatePEM(der), ownKey)
	if err != nil {
		t.Fatal(err)
	}
	cred.Leaf, _ = x509.ParseCertificate(der)
	return cred
}

// newAgedHarness starts a control plane whose authority's root was
// created a month ago, as any deployment's root predates the credentials
// it signed; issueAt can then sign credentials that expired days ago.
func newAgedHarness(t *testing.T, st enroll.Store) *harness {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := ca.NewSerial()
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().Add(-30 * 24 * time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "Innerwall Root"},
		NotBefore: created, NotAfter: created.Add(fileca.DefaultRootTTL),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileca.KeyFile), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileca.CertFile), ca.EncodeCertificatePEM(der), 0o600); err != nil {
		t.Fatal(err)
	}
	authority, err := fileca.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarnessWith(t, st, authority)
	h.authorityDir = dir
	return h
}

// refusedSync opens a sync stream with cred and returns the refusal.
func refusedSync(ctx context.Context, t *testing.T, h *harness, cred *tls.Certificate) error {
	t.Helper()
	stream, err := innerwallv1.NewAgentServiceClient(h.dial(t, cred)).Sync(ctx)
	if err != nil {
		return err
	}
	_, err = stream.Recv()
	return err
}

// refusedFlows opens a flow report with cred and returns the refusal.
func refusedFlows(ctx context.Context, t *testing.T, h *harness, cred *tls.Certificate) error {
	t.Helper()
	rf, err := innerwallv1.NewAgentServiceClient(h.dial(t, cred)).ReportFlows(ctx)
	if err != nil {
		return err
	}
	_, err = rf.CloseAndRecv()
	return err
}

func wantRefusal(t *testing.T, what string, err error, mentions string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Fatalf("%s: err = %v, want Unauthenticated", what, err)
	}
	if !strings.Contains(st.Message(), mentions) {
		t.Fatalf("%s: message %q does not say %q", what, st.Message(), mentions)
	}
}

// TestExpiredCredentialMayOnlyRenewWithinGrace is the grace end to end
// against Postgres and a real TLS listener (ADR-0016 as amended): a
// credential expired an hour ago is refused on the sync stream and on
// flow reports, renews, and the late renewal is recorded on the workload;
// the renewed credential then serves the stream. A credential expired
// beyond the grace is refused everywhere, renewal included, with a
// message naming re-enrollment in the agent's own words.
func TestExpiredCredentialMayOnlyRenewWithinGrace(t *testing.T) {
	st := storetest.Open(t)
	h := newAgedHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if credential.ReenrollRequired != enroll.ReenrollRequired {
		t.Fatalf("the agent says %q and the control plane %q; they must agree", credential.ReenrollRequired, enroll.ReenrollRequired)
	}

	plain, _, err := h.service.MintToken(ctx, "grace", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	agent := enrollAgent(t, h, plain, "grace-1", "10.9.0.1/24")
	key, err := agent.cred.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	_, trust, err := agent.cred.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Expired an hour ago: renew only.
	expiredAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	expired := h.issueAt(t, key, agent.id, expiredAt)
	wantRefusal(t, "sync with an expired credential", refusedSync(ctx, t, h, &expired), "may only renew")
	wantRefusal(t, "flows with an expired credential", refusedFlows(ctx, t, h, &expired), "may only renew")

	renewed, err := credential.Renew(ctx, key, credential.RenewOptions{Server: h.addr, Credential: expired, Trust: trust})
	if err != nil {
		t.Fatalf("renewal an hour after expiry: %v", err)
	}
	if renewed.WorkloadID != agent.id {
		t.Fatalf("renewed identity = %v, want %v", renewed.WorkloadID, agent.id)
	}
	newCert := parsePEMCert(t, renewed.CertificatePEM)
	if !time.Now().Before(newCert.NotAfter) {
		t.Fatalf("renewed credential expires %v", newCert.NotAfter)
	}
	w, err := st.LookupWorkload(ctx, agent.id)
	if err != nil {
		t.Fatal(err)
	}
	if w.LateRenewal == nil || !w.LateRenewal.ExpiredAt.Equal(expiredAt) || w.LateRenewal.RenewedAt.Sub(expiredAt) < time.Hour {
		t.Fatalf("late renewal = %+v, want one for the credential that expired at %v", w.LateRenewal, expiredAt)
	}
	if !w.CredentialExpiresAt.Equal(newCert.NotAfter) {
		t.Fatalf("recorded expiry %v, want %v", w.CredentialExpiresAt, newCert.NotAfter)
	}

	// The fresh credential serves everything again.
	if err := agent.cred.ReplaceCertificate(renewed.CertificatePEM, renewed.BundlePEM); err != nil {
		t.Fatal(err)
	}
	agent.connect(0, facts("grace-1", "10.9.0.1/24"))
	agent.expectHelloAck()
	agent.disconnect()

	// Beyond the grace: refused everywhere, naming re-enrollment.
	stale := h.issueAt(t, key, agent.id, time.Now().Add(-(h.service.Grace() + time.Hour)))
	_, err = credential.Renew(ctx, key, credential.RenewOptions{Server: h.addr, Credential: stale, Trust: trust})
	wantRefusal(t, "renewal beyond the grace", err, enroll.ReenrollRequired)
	wantRefusal(t, "sync beyond the grace", refusedSync(ctx, t, h, &stale), enroll.ReenrollRequired)
	wantRefusal(t, "flows beyond the grace", refusedFlows(ctx, t, h, &stale), enroll.ReenrollRequired)
	if w2, _ := st.LookupWorkload(ctx, agent.id); !w2.CredentialExpiresAt.Equal(newCert.NotAfter) {
		t.Fatal("a refused renewal was recorded")
	}

	// A credential whose validity has not begun is refused too: the
	// handshake no longer judges time, so the interceptor must.
	early := h.issueAt(t, key, agent.id, time.Now().Add(48*time.Hour))
	_, err = credential.Renew(ctx, key, credential.RenewOptions{Server: h.addr, Credential: early, Trust: trust})
	wantRefusal(t, "renewal before validity", err, "not yet valid")
}
