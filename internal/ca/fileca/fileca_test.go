package fileca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

func newCSR(t *testing.T, tmpl *x509.CertificateRequest) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), key
}

func parseCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block in certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func poolFrom(t *testing.T, bundle []byte) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		t.Fatal("bundle holds no certificates")
	}
	return pool
}

func TestInitCreatesFilesWithRestrictedModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	if _, err := Init(dir, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 700", st.Mode().Perm())
	}
	st, err = os.Stat(filepath.Join(dir, KeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o, want 600", st.Mode().Perm())
	}
	if _, err := Init(dir, InitOptions{}); !errors.Is(err, ErrExists) {
		t.Fatalf("second Init err = %v, want ErrExists", err)
	}
	if _, err := Open(t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open(empty) err = %v, want ErrNotFound", err)
	}
}

func TestSignVerifiesAgainstBundleAndHonorsTTL(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A fixed instant inside the root's validity, derived from the root
	// rather than the calendar so the test does not expire.
	fixed := a.cert.NotBefore.Add(time.Hour).Truncate(time.Second)
	a.now = func() time.Time { return fixed }

	id, _ := identity.NewWorkloadID()
	csr, key := newCSR(t, &x509.CertificateRequest{})
	certPEM, err := a.Sign(context.Background(), csr, id, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cert := parseCert(t, certPEM)

	bundle, err := a.Bundle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:       poolFrom(t, bundle),
		CurrentTime: fixed.Add(time.Hour),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("issued certificate does not verify against bundle: %v", err)
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		t.Fatal("certificate does not carry the CSR's public key")
	}
	if !cert.NotAfter.Equal(fixed.Add(24 * time.Hour)) {
		t.Fatalf("NotAfter = %v, want %v", cert.NotAfter, fixed.Add(24*time.Hour))
	}
	if !cert.NotBefore.Equal(fixed.Add(-ca.BackdateNotBefore)) {
		t.Fatalf("NotBefore = %v", cert.NotBefore)
	}
	got, err := identity.FromCertificate(cert)
	if err != nil || got != id {
		t.Fatalf("identity from cert = %v, %v; want %v", got, err, id)
	}
	if cert.Subject.CommonName != id.String() {
		t.Fatalf("CN = %q, want %q", cert.Subject.CommonName, id)
	}

	// The TTL is a ceiling the root imposes too.
	if _, err := a.Sign(context.Background(), csr, id, DefaultRootTTL+time.Hour); err == nil {
		t.Fatal("ttl beyond root accepted")
	}
	// Each signature gets a fresh serial.
	again := parseCert(t, mustSign(t, a, csr, id))
	if again.SerialNumber.Cmp(cert.SerialNumber) == 0 {
		t.Fatal("serial reused")
	}
}

func mustSign(t *testing.T, a *Authority, csr []byte, id identity.WorkloadID) []byte {
	t.Helper()
	out, err := a.Sign(context.Background(), csr, id, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestHostileCSRContributesOnlyItsPublicKey is the executable statement of
// ADR-0004 and ADR-0016 at the signing boundary: the enrollee may ask for
// anything in its CSR and receives a certificate containing only what the
// control plane granted.
func TestHostileCSRContributesOnlyItsPublicKey(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir, InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	victim, _ := identity.NewWorkloadID()
	granted, _ := identity.NewWorkloadID()

	// Everything an attacker could plausibly put in a request.
	hostile := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:         victim.String(),
			Organization:       []string{"control plane"},
			OrganizationalUnit: []string{"admin"},
			Country:            []string{"XX"},
		},
		DNSNames:       []string{"control-plane.internal", "*.internal"},
		EmailAddresses: []string{"root@internal"},
		IPAddresses:    []net.IP{net.IPv4(10, 0, 0, 1)},
		URIs:           []*url.URL{victim.URI(), mustURL("other://control-plane/admin")},
		ExtraExtensions: []pkix.Extension{
			// basicConstraints CA:TRUE
			{Id: asn1.ObjectIdentifier{2, 5, 29, 19}, Critical: true, Value: []byte{0x30, 0x03, 0x01, 0x01, 0xff}},
			// A private-arc extension with arbitrary content.
			{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}, Value: []byte("labels=env:prod,role:db")},
		},
	}
	csr, key := newCSR(t, hostile)
	cert := parseCert(t, mustSign(t, a, csr, granted))

	if got, err := identity.FromCertificate(cert); err != nil || got != granted {
		t.Fatalf("identity = %v, %v; want %v", got, err, granted)
	}
	if len(cert.URIs) != 1 || cert.URIs[0].String() != granted.URI().String() {
		t.Fatalf("URIs = %v, want exactly %v", cert.URIs, granted.URI())
	}
	if len(cert.DNSNames) != 0 || len(cert.EmailAddresses) != 0 || len(cert.IPAddresses) != 0 {
		t.Fatalf("requested SANs leaked: dns=%v email=%v ip=%v", cert.DNSNames, cert.EmailAddresses, cert.IPAddresses)
	}
	if cert.Subject.CommonName != granted.String() {
		t.Fatalf("CN = %q, want %q", cert.Subject.CommonName, granted)
	}
	if len(cert.Subject.Organization) != 0 || len(cert.Subject.OrganizationalUnit) != 0 || len(cert.Subject.Country) != 0 {
		t.Fatalf("requested subject leaked: %v", cert.Subject)
	}
	if cert.IsCA {
		t.Fatal("requested CA:TRUE leaked")
	}
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}) {
			t.Fatal("requested private extension leaked")
		}
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("ExtKeyUsage = %v, want client auth only", cert.ExtKeyUsage)
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		t.Fatal("certificate does not carry the CSR public key")
	}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestIssueServerCertificate(t *testing.T) {
	a, err := Init(t.TempDir(), InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, err := a.IssueServerCertificate([]string{"localhost", "127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cert := parseCert(t, certPEM)
	bundle, _ := a.Bundle(context.Background())
	if _, err := cert.Verify(x509.VerifyOptions{Roots: poolFrom(t, bundle), DNSName: "localhost"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: poolFrom(t, bundle), DNSName: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := pem.Decode(keyPEM); b == nil || b.Type != "PRIVATE KEY" {
		t.Fatal("server key is not a PRIVATE KEY block")
	}
}
