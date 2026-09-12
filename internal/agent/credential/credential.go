// Package credential is the agent's side of enrollment and renewal: it
// generates the workload keypair, builds certificate signing requests,
// calls the control plane, and keeps the resulting credential in a state
// directory the daemon reads (ADR-0020).
//
// The private key is generated here and never leaves the host. The
// control plane sees only a CSR, and the CSR contributes only its public
// key; identity is granted by the control plane.
package credential

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

const (
	// KeyFile holds the workload private key (PKCS#8 PEM).
	KeyFile = "workload.key"
	// CertFile holds the current workload certificate (PEM).
	CertFile = "workload.crt"
	// BundleFile holds the authority bundle the control plane is verified with.
	BundleFile = "ca-bundle.crt"

	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// ErrNotEnrolled is returned by Load when the state directory holds no
// credential.
var ErrNotEnrolled = errors.New("credential: not enrolled")

// GenerateKey creates the workload keypair.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("credential: generating key: %w", err)
	}
	return key, nil
}

// NewCSR builds a PEM-encoded PKCS#10 request over key. The request names
// nothing: no subject, no SANs. The control plane ignores those fields by
// rule, and an empty request makes that rule visible from the agent side.
func NewCSR(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		return nil, fmt.Errorf("credential: building csr: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

// Store is the on-disk credential state of one agent.
type Store struct {
	Dir string
}

func (s Store) keyPath() string    { return filepath.Join(s.Dir, KeyFile) }
func (s Store) certPath() string   { return filepath.Join(s.Dir, CertFile) }
func (s Store) bundlePath() string { return filepath.Join(s.Dir, BundleFile) }

// Save writes a freshly enrolled credential: key, certificate, and bundle.
// The directory is created with mode 0700 and every file with mode 0600.
func (s Store) Save(key *ecdsa.PrivateKey, certPEM, bundlePEM []byte) error {
	if err := os.MkdirAll(s.Dir, dirMode); err != nil {
		return fmt.Errorf("credential: creating %s: %w", s.Dir, err)
	}
	if err := os.Chmod(s.Dir, dirMode); err != nil {
		return fmt.Errorf("credential: restricting %s: %w", s.Dir, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("credential: encoding key: %w", err)
	}
	keyPEM := encodePEM("PRIVATE KEY", keyDER)
	if err := writeAtomic(s.keyPath(), keyPEM); err != nil {
		return err
	}
	if err := writeAtomic(s.bundlePath(), bundlePEM); err != nil {
		return err
	}
	return writeAtomic(s.certPath(), certPEM)
}

// ReplaceCertificate swaps in a renewed certificate (and refreshed bundle)
// atomically: a reader sees either the old file or the new one, never a
// partial write. The key is untouched.
func (s Store) ReplaceCertificate(certPEM, bundlePEM []byte) error {
	if len(bundlePEM) > 0 {
		if err := writeAtomic(s.bundlePath(), bundlePEM); err != nil {
			return err
		}
	}
	return writeAtomic(s.certPath(), certPEM)
}

// Load reads the stored credential and bundle.
func (s Store) Load() (cred tls.Certificate, bundlePEM []byte, err error) {
	certPEM, err := os.ReadFile(s.certPath())
	if errors.Is(err, os.ErrNotExist) {
		return tls.Certificate{}, nil, ErrNotEnrolled
	}
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("credential: reading certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(s.keyPath())
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("credential: reading key: %w", err)
	}
	cred, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("credential: loading key pair: %w", err)
	}
	cred.Leaf, err = x509.ParseCertificate(cred.Certificate[0])
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("credential: parsing certificate: %w", err)
	}
	bundlePEM, err = os.ReadFile(s.bundlePath())
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("credential: reading bundle: %w", err)
	}
	return cred, bundlePEM, nil
}

// LoadKey reads only the private key.
func (s Store) LoadKey() (*ecdsa.PrivateKey, error) {
	keyPEM, err := os.ReadFile(s.keyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("credential: reading key: %w", err)
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("credential: key file is not a PRIVATE KEY PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("credential: parsing key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("credential: key is %T, want ecdsa", parsed)
	}
	return key, nil
}

func encodePEM(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

// writeAtomic writes data to a temporary file beside path with mode 0600
// and renames it into place.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("credential: creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("credential: restricting %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("credential: writing %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("credential: syncing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("credential: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("credential: replacing %s: %w", path, err)
	}
	return nil
}

// EnrollOptions configure one enrollment call.
type EnrollOptions struct {
	// Server is the control-plane address, host:port.
	Server string
	// Token is the provisioning token plaintext.
	Token string
	// BootstrapCA is the PEM trust anchor for the control plane, delivered
	// out of band together with the token. At enrollment the agent holds
	// nothing that could verify the control plane: no credential and no
	// bundle. The token authenticates the agent to the server; this anchor
	// authenticates the server to the agent. Without it the token would be
	// sent to whatever answers at Server. Empty means the host's system
	// trust store, which is right when the control plane presents a
	// certificate the host already trusts.
	BootstrapCA []byte
	// Facts are descriptive host facts sent alongside; they never influence
	// identity or authorization.
	Facts *innerwallv1.HostFacts
}

// EnrollResult is what the control plane granted.
type EnrollResult struct {
	WorkloadID     identity.WorkloadID
	CertificatePEM []byte
	BundlePEM      []byte
	Labels         []*innerwallv1.Label
}

// Enroll generates nothing and persists nothing: it exchanges the token and
// a CSR over key for a credential and returns it for the caller to store.
func Enroll(ctx context.Context, key *ecdsa.PrivateKey, opts EnrollOptions) (*EnrollResult, error) {
	csr, err := NewCSR(key)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := TLSConfig(nil, opts.BootstrapCA)
	if err != nil {
		return nil, err
	}
	conn, err := dial(opts.Server, tlsCfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	resp, err := innerwallv1.NewEnrollmentServiceClient(conn).Enroll(ctx, &innerwallv1.EnrollRequest{
		ProvisioningToken: opts.Token,
		CsrPem:            csr,
		Facts:             opts.Facts,
	})
	if err != nil {
		return nil, fmt.Errorf("credential: enroll: %w", err)
	}
	// The identity that matters is the one in the certificate; the
	// workload_id field is diagnostic. Check they agree so a disagreement
	// is caught here rather than at first use.
	cert, err := parseCert(resp.GetCertificatePem())
	if err != nil {
		return nil, err
	}
	id, err := identity.FromCertificate(cert)
	if err != nil {
		return nil, fmt.Errorf("credential: issued certificate: %w", err)
	}
	if resp.GetWorkloadId() != id.String() {
		return nil, fmt.Errorf("credential: response names workload %q but certificate binds %v", resp.GetWorkloadId(), id)
	}
	if err := verifyAgainst(cert, resp.GetCaBundlePem()); err != nil {
		return nil, err
	}
	return &EnrollResult{
		WorkloadID:     id,
		CertificatePEM: resp.GetCertificatePem(),
		BundlePEM:      resp.GetCaBundlePem(),
		Labels:         resp.GetAssignedLabels(),
	}, nil
}

// RenewOptions configure one renewal call.
type RenewOptions struct {
	Server string
	// Credential is the current workload credential, presented over mTLS.
	Credential tls.Certificate
	// Trust is the authority bundle stored at enrollment.
	Trust []byte
}

// RenewResult is a renewed credential.
type RenewResult struct {
	WorkloadID     identity.WorkloadID
	CertificatePEM []byte
	BundlePEM      []byte
}

// Renew asks for a fresh certificate over the current key, authenticated by
// the current credential. The control plane binds the same identity; the
// caller checks nothing about which workload because it cannot choose.
func Renew(ctx context.Context, key *ecdsa.PrivateKey, opts RenewOptions) (*RenewResult, error) {
	csr, err := NewCSR(key)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := TLSConfig(&opts.Credential, opts.Trust)
	if err != nil {
		return nil, err
	}
	conn, err := dial(opts.Server, tlsCfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	resp, err := innerwallv1.NewAgentServiceClient(conn).RenewCredential(ctx, &innerwallv1.RenewCredentialRequest{CsrPem: csr})
	if err != nil {
		return nil, fmt.Errorf("credential: renew: %w", err)
	}
	cert, err := parseCert(resp.GetCertificatePem())
	if err != nil {
		return nil, err
	}
	id, err := identity.FromCertificate(cert)
	if err != nil {
		return nil, fmt.Errorf("credential: renewed certificate: %w", err)
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return nil, errors.New("credential: renewed certificate does not carry this key")
	}
	if err := verifyAgainst(cert, resp.GetCaBundlePem()); err != nil {
		return nil, err
	}
	return &RenewResult{WorkloadID: id, CertificatePEM: resp.GetCertificatePem(), BundlePEM: resp.GetCaBundlePem()}, nil
}

// TLSConfig builds the agent's client configuration: the workload
// credential (nil for enrollment, which has none yet) and the PEM trust
// anchor for the control plane (empty means the host's trust store).
func TLSConfig(cred *tls.Certificate, trustPEM []byte) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS13}
	if len(trustPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(trustPEM) {
			return nil, errors.New("credential: trust bundle holds no certificates")
		}
		cfg.RootCAs = pool
	}
	if cred != nil {
		cfg.Certificates = []tls.Certificate{*cred}
	}
	return cfg, nil
}

func dial(server string, tlsCfg *tls.Config) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(server, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return nil, fmt.Errorf("credential: dialing %s: %w", server, err)
	}
	return conn, nil
}

func parseCert(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("credential: response holds no CERTIFICATE PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("credential: parsing issued certificate: %w", err)
	}
	return cert, nil
}

// verifyAgainst checks the issued certificate chains to the returned bundle,
// so a control plane that hands out a credential its own bundle would not
// accept is caught before the credential is stored.
func verifyAgainst(cert *x509.Certificate, bundlePEM []byte) error {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundlePEM) {
		return errors.New("credential: response bundle holds no certificates")
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return fmt.Errorf("credential: issued certificate does not verify against the returned bundle: %w", err)
	}
	return nil
}
