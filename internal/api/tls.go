package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The files a generated operator certificate is persisted as, beside the
// authority directory's other operator-provisioned configuration.
const (
	OperatorCertFile = "operator.crt"
	OperatorKeyFile  = "operator.key"
)

// selfSignedTTL is the lifetime of a generated certificate: just over a
// year, the longest a browser accepts without complaint, after which the
// control plane generates a fresh one on start and logs its fingerprint.
const selfSignedTTL = 397 * 24 * time.Hour

// clockSkew backdates a generated certificate so a browser on a slightly
// slow clock accepts it immediately.
const clockSkew = 5 * time.Minute

// ListenerCertificate returns the operator listener's certificate. When
// certFile and keyFile are both set they are loaded and must be valid;
// when both are empty a self-signed certificate persisted in dir is used,
// generated on first start or when the persisted one has expired, and its
// SHA-256 fingerprint is logged either way so the first connection can be
// verified out of band (ADR-0021). Setting one of the two paths without
// the other is a configuration error.
func ListenerCertificate(certFile, keyFile, dir string, hosts []string, now time.Time, log *slog.Logger) (tls.Certificate, error) {
	switch {
	case certFile != "" && keyFile != "":
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("loading operator certificate: %w", err)
		}
		logCertificate(log, cert, certFile, false)
		return cert, nil
	case certFile != "" || keyFile != "":
		return tls.Certificate{}, errors.New("operator TLS certificate and key must be configured together")
	}
	certPath := filepath.Join(dir, OperatorCertFile)
	keyPath := filepath.Join(dir, OperatorKeyFile)
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	switch {
	case err == nil && cert.Leaf != nil && cert.Leaf.NotAfter.After(now):
		logCertificate(log, cert, certPath, false)
		return cert, nil
	case err == nil:
		log.Warn("operator listener certificate has expired; generating a new one", "path", certPath, "expired", cert.Leaf.NotAfter.UTC().Format(time.RFC3339))
	case errors.Is(err, os.ErrNotExist):
	default:
		return tls.Certificate{}, fmt.Errorf("loading operator certificate from %s: %w", dir, err)
	}
	cert, err = generateSelfSigned(certPath, keyPath, hosts, now)
	if err != nil {
		return tls.Certificate{}, err
	}
	logCertificate(log, cert, certPath, true)
	return cert, nil
}

// generateSelfSigned writes a fresh self-signed certificate and key.
func generateSelfSigned(certPath, keyPath string, hosts []string, now time.Time) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating operator key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "innerwall operator surface"},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(selfSignedTTL),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("creating operator certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("encoding operator key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("writing operator key: %w", err)
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil { //nolint:gosec // a certificate is public
		return tls.Certificate{}, fmt.Errorf("writing operator certificate: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("loading generated operator certificate: %w", err)
	}
	return cert, nil
}

// Fingerprint is the SHA-256 digest of a certificate's DER encoding, in
// the colon-separated form a browser's certificate viewer shows.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	hexed := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(sum))
	for i := 0; i < len(hexed); i += 2 {
		parts = append(parts, hexed[i:i+2])
	}
	return strings.Join(parts, ":")
}

func logCertificate(log *slog.Logger, cert tls.Certificate, path string, generated bool) {
	if len(cert.Certificate) == 0 {
		return
	}
	attrs := []any{"path", path, "sha256_fingerprint", Fingerprint(cert.Certificate[0]), "generated", generated}
	if cert.Leaf != nil {
		attrs = append(attrs, "expires", cert.Leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	log.Info("operator listener certificate", attrs...)
}

// ServerTLSConfig is the operator listener's TLS configuration: the
// certificate above, current TLS only, and both HTTP versions offered.
func ServerTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h2", "http/1.1"},
	}
}
