package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
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
