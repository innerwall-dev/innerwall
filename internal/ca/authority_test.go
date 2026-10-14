package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/identity"
)

func csrPEM(t *testing.T, tmpl *x509.CertificateRequest) ([]byte, *ecdsa.PrivateKey) {
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

func TestPublicKeyFromCSR(t *testing.T) {
	pemBytes, key := csrPEM(t, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "anything"}})
	pub, err := PublicKeyFromCSR(pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !key.PublicKey.Equal(pub) {
		t.Fatal("returned key does not match the CSR key")
	}

	bad := map[string][]byte{
		"empty":       nil,
		"garbage":     []byte("not pem"),
		"wrong type":  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1}}),
		"two blocks":  append(append([]byte{}, pemBytes...), pemBytes...),
		"corrupt der": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: []byte{0x30, 0x01}}),
	}
	for name, in := range bad {
		if _, err := PublicKeyFromCSR(in); !errors.Is(err, ErrBadCSR) {
			t.Errorf("%s: err = %v, want ErrBadCSR", name, err)
		}
	}
}

func TestPublicKeyFromCSRRejectsBadSignature(t *testing.T) {
	pemBytes, _ := csrPEM(t, &x509.CertificateRequest{})
	block, _ := pem.Decode(pemBytes)
	// Flip a byte near the end, inside the signature.
	block.Bytes[len(block.Bytes)-3] ^= 0xff
	tampered := pem.EncodeToMemory(block)
	if _, err := PublicKeyFromCSR(tampered); !errors.Is(err, ErrBadCSR) {
		t.Fatalf("err = %v, want ErrBadCSR", err)
	}
}

func TestLeafTemplate(t *testing.T) {
	id, _ := identity.NewWorkloadID()
	now := time.Now()
	tmpl, err := LeafTemplate(id, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpl.URIs) != 1 || tmpl.URIs[0].String() != id.URI().String() {
		t.Fatalf("uris = %v", tmpl.URIs)
	}
	if tmpl.Subject.CommonName != id.String() {
		t.Fatalf("cn = %q", tmpl.Subject.CommonName)
	}
	if !tmpl.NotBefore.Equal(now.Add(-BackdateNotBefore)) || !tmpl.NotAfter.Equal(now.Add(time.Hour)) {
		t.Fatalf("validity = %v..%v", tmpl.NotBefore, tmpl.NotAfter)
	}
	if tmpl.IsCA {
		t.Fatal("leaf template must not be a CA")
	}
	if _, err := LeafTemplate(identity.WorkloadID{}, now, time.Hour); err == nil {
		t.Fatal("zero identity accepted")
	}
	if _, err := LeafTemplate(id, now, 0); err == nil {
		t.Fatal("zero ttl accepted")
	}
}

// TestLeafIdentityKeysLikeTheDirectivePayload signs leaves from
// LeafTemplate, the one place a workload SAN is built, and checks that the
// identity the gateway's interceptor reads back from each (FromCertificate,
// which keys the stream registry) is byte-equal to the identity the
// notification listener parses from a directive's payload
// (ParseWorkloadID of WorkloadID.String), so a directive finds the stream
// whatever the uuid's digits (issue #106).
func TestLeafIdentityKeysLikeTheDirectivePayload(t *testing.T) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registry := map[identity.WorkloadID]int{}
	for i := range 200 {
		id, err := identity.NewWorkloadID()
		if err != nil {
			t.Fatal(err)
		}
		tmpl, err := LeafTemplate(id, time.Now(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &leafKey.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		leaf, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		fromCert, err := identity.FromCertificate(leaf)
		if err != nil {
			t.Fatalf("leaf %d: %v", i, err)
		}
		fromPayload, err := identity.ParseWorkloadID(id.String())
		if err != nil {
			t.Fatal(err)
		}
		if fromCert != fromPayload || fromCert.UUID() != fromPayload.UUID() {
			t.Fatalf("leaf %d: certificate identity %v, payload identity %v", i, fromCert, fromPayload)
		}
		registry[fromCert] = i
		if got, ok := registry[fromPayload]; !ok || got != i {
			t.Fatalf("leaf %d: the payload identity does not find the certificate identity's entry", i)
		}
	}
}
