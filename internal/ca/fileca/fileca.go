// Package fileca is the file-backed signing authority: a private key and a
// self-signed root certificate on local disk. It is the single Authority
// implementation shipped in v1 and is suitable for development and for small
// deployments where the control-plane host is itself the trust boundary.
// Backends that keep the key elsewhere implement the same ca.Authority
// interface; nothing outside this package depends on the files it keeps.
package fileca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

const (
	// KeyFile is the name of the root private key within the CA directory.
	KeyFile = "ca.key"
	// CertFile is the name of the root certificate within the CA directory.
	CertFile = "ca.crt"

	// DefaultRootTTL is the lifetime of a generated root. The root is
	// long-lived because rotating it means re-enrolling every workload;
	// the credentials it issues are short-lived (ADR-0016).
	DefaultRootTTL = 10 * 365 * 24 * time.Hour

	dirMode  os.FileMode = 0o700
	keyMode  os.FileMode = 0o600
	certMode os.FileMode = 0o644
)

// ErrExists is returned by Init when the directory already holds a CA.
var ErrExists = errors.New("fileca: directory already contains a certificate authority")

// ErrNotFound is returned by Open when the directory holds no CA.
var ErrNotFound = errors.New("fileca: no certificate authority in directory")

// Authority signs with a key held on local disk.
type Authority struct {
	key  *ecdsa.PrivateKey
	cert *x509.Certificate
	pem  []byte
	now  func() time.Time
}

var _ ca.Authority = (*Authority)(nil)

// InitOptions control root generation.
type InitOptions struct {
	// CommonName of the root; informational only.
	CommonName string
	// TTL of the root certificate; DefaultRootTTL if zero.
	TTL time.Duration
}

// Init generates a new root key and self-signed certificate in dir, creating
// the directory with mode 0700 if needed. The key file is written with mode
// 0600. It refuses to overwrite an existing CA.
func Init(dir string, opts InitOptions) (*Authority, error) {
	if opts.TTL == 0 {
		opts.TTL = DefaultRootTTL
	}
	if opts.CommonName == "" {
		opts.CommonName = "Innerwall Root"
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("fileca: creating %s: %w", dir, err)
	}
	keyPath, certPath := filepath.Join(dir, KeyFile), filepath.Join(dir, CertFile)
	for _, p := range []string{keyPath, certPath} {
		if _, err := os.Stat(p); err == nil {
			return nil, fmt.Errorf("%w: %s", ErrExists, p)
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("fileca: generating root key: %w", err)
	}
	serial, err := ca.NewSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: opts.CommonName},
		NotBefore:             now.Add(-ca.BackdateNotBefore),
		NotAfter:              now.Add(opts.TTL),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("fileca: self-signing root: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("fileca: encoding root key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := ca.EncodeCertificatePEM(der)

	if err := writeExclusive(keyPath, keyPEM, keyMode); err != nil {
		return nil, err
	}
	if err := writeExclusive(certPath, certPEM, certMode); err != nil {
		return nil, err
	}
	return Open(dir)
}

// Open loads the CA from dir.
func Open(dir string) (*Authority, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, KeyFile)) //nolint:gosec // operator-configured directory
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, dir)
	}
	if err != nil {
		return nil, fmt.Errorf("fileca: reading root key: %w", err)
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, CertFile)) //nolint:gosec // operator-configured directory
	if err != nil {
		return nil, fmt.Errorf("fileca: reading root certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" {
		return nil, errors.New("fileca: root key is not a PRIVATE KEY PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fileca: parsing root key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("fileca: root key is %T, want ecdsa", parsed)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, errors.New("fileca: root certificate is not a CERTIFICATE PEM block")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fileca: parsing root certificate: %w", err)
	}
	if !cert.IsCA {
		return nil, errors.New("fileca: root certificate is not a CA")
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return nil, errors.New("fileca: root key does not match root certificate")
	}
	return &Authority{key: key, cert: cert, pem: ca.EncodeCertificatePEM(cert.Raw), now: time.Now}, nil
}

// Sign implements ca.Authority. The CSR contributes its public key and
// nothing else; the certificate's subject and SANs come from LeafTemplate,
// which is built from the server-granted identity alone.
func (a *Authority) Sign(_ context.Context, csrPEM []byte, id identity.WorkloadID, ttl time.Duration) ([]byte, error) {
	pub, err := ca.PublicKeyFromCSR(csrPEM)
	if err != nil {
		return nil, err
	}
	now := a.now()
	tmpl, err := ca.LeafTemplate(id, now, ttl)
	if err != nil {
		return nil, err
	}
	if tmpl.NotAfter.After(a.cert.NotAfter) {
		return nil, fmt.Errorf("fileca: requested ttl outlives the root (expires %s)", a.cert.NotAfter.Format(time.RFC3339))
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, pub, a.key)
	if err != nil {
		return nil, fmt.Errorf("fileca: signing: %w", err)
	}
	return ca.EncodeCertificatePEM(der), nil
}

// Bundle implements ca.Authority.
func (a *Authority) Bundle(context.Context) ([]byte, error) {
	out := make([]byte, len(a.pem))
	copy(out, a.pem)
	return out, nil
}

// IssueServerCertificate issues a TLS server certificate for the control
// plane's own listener, signed by this root, so agents can verify the server
// with the same bundle they use for everything else. Hosts may be DNS names
// or IP addresses. The returned key is a fresh PKCS#8 key; the root key is
// never reused for a leaf.
func (a *Authority) IssueServerCertificate(hosts []string, ttl time.Duration) (certPEM, keyPEM []byte, err error) {
	if len(hosts) == 0 {
		return nil, nil, errors.New("fileca: server certificate needs at least one host")
	}
	if ttl <= 0 {
		return nil, nil, errors.New("fileca: server certificate ttl must be positive")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("fileca: generating server key: %w", err)
	}
	serial, err := ca.NewSerial()
	if err != nil {
		return nil, nil, err
	}
	now := a.now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hosts[0]},
		NotBefore:             now.Add(-ca.BackdateNotBefore),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, nil, fmt.Errorf("fileca: signing server certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("fileca: encoding server key: %w", err)
	}
	return ca.EncodeCertificatePEM(der), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

// writeExclusive creates path with mode, failing if it already exists. The
// mode is applied at creation so the key never exists with a wider mode,
// even briefly.
func writeExclusive(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) //nolint:gosec // path is within the operator-configured directory
	if err != nil {
		return fmt.Errorf("fileca: creating %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("fileca: writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("fileca: closing %s: %w", path, err)
	}
	return nil
}
