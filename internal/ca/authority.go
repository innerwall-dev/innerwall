package ca

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"time"

	"github.com/innerwall-dev/innerwall/internal/identity"
)

// Authority is the signing boundary for workload credentials. Enrollment and
// renewal are written against this interface so that a backend holding its
// key elsewhere (a secrets manager, a hardware module, a hosted signing
// service) replaces the file-backed default without touching enrollment
// logic (ADR-0020).
type Authority interface {
	// Sign issues a client certificate binding id to the public key in the
	// CSR. Implementations MUST use only the CSR's public key: any subject,
	// SANs, or extensions requested in the CSR are ignored. Identity is
	// granted by the control plane, never requested by the enrollee.
	Sign(ctx context.Context, csrPEM []byte, id identity.WorkloadID, ttl time.Duration) (certPEM []byte, err error)
	// Bundle returns the PEM trust chain for verifying issued credentials.
	Bundle(ctx context.Context) ([]byte, error)
}

// ErrBadCSR is wrapped by every CSR rejection so callers can map it to a
// client error without inspecting the message.
var ErrBadCSR = errors.New("ca: invalid certificate signing request")

// BackdateNotBefore is how far into the past an issued certificate's
// validity starts. Hosts with a slightly slow clock would otherwise reject a
// freshly issued credential as not yet valid.
const BackdateNotBefore = 5 * time.Minute

// PublicKeyFromCSR parses a PEM-encoded PKCS#10 request and returns its
// public key and nothing else. The signature is checked so the enrollee has
// proven possession of the private key; the subject, requested SANs, and
// requested extensions are deliberately not returned. This function is the
// only way an Authority implementation should read a CSR: what it does not
// return cannot leak into a certificate.
func PublicKeyFromCSR(csrPEM []byte) (crypto.PublicKey, error) {
	block, rest := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("%w: expected one CERTIFICATE REQUEST PEM block", ErrBadCSR)
	}
	if extra, _ := pem.Decode(rest); extra != nil {
		return nil, fmt.Errorf("%w: expected exactly one PEM block", ErrBadCSR)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadCSR, err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("%w: signature does not verify: %w", ErrBadCSR, err)
	}
	if err := checkKey(csr.PublicKey); err != nil {
		return nil, err
	}
	return csr.PublicKey, nil
}

// checkKey rejects key types and sizes too weak to carry an identity.
func checkKey(pub crypto.PublicKey) error {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if k.Curve == nil || k.Params().BitSize < 256 {
			return fmt.Errorf("%w: ecdsa curve too small", ErrBadCSR)
		}
	case ed25519.PublicKey:
	case *rsa.PublicKey:
		if k.N.BitLen() < 2048 {
			return fmt.Errorf("%w: rsa key must be at least 2048 bits", ErrBadCSR)
		}
	default:
		return fmt.Errorf("%w: unsupported public key type %T", ErrBadCSR, pub)
	}
	return nil
}

// LeafTemplate builds the certificate template for a workload credential.
// The template is constructed entirely from server-side values: the identity
// goes in exactly one URI SAN, the common name mirrors it for human readers
// and is never read for authorization, and no DNS, IP, or email SANs and no
// extra extensions are present. Implementations sign this template with the
// public key from PublicKeyFromCSR and change nothing else.
func LeafTemplate(id identity.WorkloadID, now time.Time, ttl time.Duration) (*x509.Certificate, error) {
	if id.IsZero() {
		return nil, errors.New("ca: refusing to issue a certificate for the zero identity")
	}
	if ttl <= 0 {
		return nil, errors.New("ca: ttl must be positive")
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	return &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: id.String()},
		URIs:                  []*url.URL{id.URI()},
		NotBefore:             now.Add(-BackdateNotBefore),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}, nil
}

// newSerial draws a 128-bit random serial. Serials must be unique per
// issuer; randomness of this width makes a collision a non-event and keeps
// issuance stateless.
func newSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("ca: generating serial: %w", err)
	}
	return serial, nil
}

// NewSerial is exported for implementations that build templates other than
// workload leaves (a server certificate for the listener, for example).
func NewSerial() (*big.Int, error) { return newSerial() }

// EncodeCertificatePEM encodes DER certificate bytes as PEM.
func EncodeCertificatePEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
