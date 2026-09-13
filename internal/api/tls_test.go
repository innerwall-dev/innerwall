package api_test

import (
	"bytes"
	"crypto/tls"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/api"
)

func TestSelfSignedCertificateIsGeneratedLoggedAndPersisted(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, nil))

	cert, err := api.ListenerCertificate("", "", dir, []string{"localhost", " 127.0.0.1", ""}, now, log)
	if err != nil {
		t.Fatal(err)
	}
	fp := api.Fingerprint(cert.Certificate[0])
	if len(fp) != 95 || strings.Count(fp, ":") != 31 {
		t.Fatalf("fingerprint form %q", fp)
	}
	first := logBuf.String()
	if !strings.Contains(first, "sha256_fingerprint="+fp) || !strings.Contains(first, "generated=true") {
		t.Fatalf("generation not logged with its fingerprint: %s", first)
	}
	if cert.Leaf == nil {
		t.Fatal("leaf not parsed")
	}
	if got := cert.Leaf.DNSNames; len(got) != 1 || got[0] != "localhost" {
		t.Fatalf("dns names %v", got)
	}
	if got := cert.Leaf.IPAddresses; len(got) != 1 || got[0].String() != "127.0.0.1" {
		t.Fatalf("ip addresses %v", got)
	}
	if cert.Leaf.NotBefore.After(now) || !cert.Leaf.NotAfter.After(now.Add(365*24*time.Hour)) {
		t.Fatalf("validity %v to %v", cert.Leaf.NotBefore, cert.Leaf.NotAfter)
	}
	keyInfo, err := os.Stat(filepath.Join(dir, api.OperatorKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v, want 0600", keyInfo.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, api.OperatorCertFile)); err != nil {
		t.Fatal(err)
	}

	// A restart loads the persisted pair: same fingerprint, logged again,
	// not regenerated.
	logBuf.Reset()
	again, err := api.ListenerCertificate("", "", dir, []string{"localhost"}, now.Add(time.Hour), log)
	if err != nil {
		t.Fatal(err)
	}
	if api.Fingerprint(again.Certificate[0]) != fp {
		t.Fatal("restart regenerated the certificate")
	}
	second := logBuf.String()
	if !strings.Contains(second, "sha256_fingerprint="+fp) || !strings.Contains(second, "generated=false") {
		t.Fatalf("restart did not log the fingerprint: %s", second)
	}

	// An expired persisted certificate is replaced and the new
	// fingerprint logged.
	logBuf.Reset()
	renewed, err := api.ListenerCertificate("", "", dir, []string{"localhost"}, cert.Leaf.NotAfter.Add(time.Second), log)
	if err != nil {
		t.Fatal(err)
	}
	if api.Fingerprint(renewed.Certificate[0]) == fp {
		t.Fatal("expired certificate was kept")
	}
	third := logBuf.String()
	if !strings.Contains(third, "expired") || !strings.Contains(third, "generated=true") || !strings.Contains(third, "sha256_fingerprint="+api.Fingerprint(renewed.Certificate[0])) {
		t.Fatalf("renewal log: %s", third)
	}

	// The listener serves it over TLS 1.3 only.
	cfg := api.ServerTLSConfig(renewed)
	if cfg.MinVersion != tls.VersionTLS13 || len(cfg.Certificates) != 1 {
		t.Fatalf("tls config %+v", cfg)
	}
}

func TestConfiguredCertificate(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	// Produce a pair to point at, in a different directory, so the
	// configured path is what is loaded and nothing is generated.
	seed := t.TempDir()
	if _, err := api.ListenerCertificate("", "", seed, []string{"console.example"}, now, log); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(seed, api.OperatorCertFile)
	keyPath := filepath.Join(seed, api.OperatorKeyFile)

	cert, err := api.ListenerCertificate(certPath, keyPath, dir, nil, now, log)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Leaf == nil || cert.Leaf.DNSNames[0] != "console.example" {
		t.Fatalf("loaded the wrong certificate: %+v", cert.Leaf)
	}
	if _, err := os.Stat(filepath.Join(dir, api.OperatorCertFile)); !os.IsNotExist(err) {
		t.Fatal("a configured pair still generated a self-signed certificate")
	}

	if _, err := api.ListenerCertificate(certPath, "", dir, nil, now, log); err == nil {
		t.Fatal("certificate without key accepted")
	}
	if _, err := api.ListenerCertificate("", keyPath, dir, nil, now, log); err == nil {
		t.Fatal("key without certificate accepted")
	}
	if _, err := api.ListenerCertificate(filepath.Join(dir, "missing.crt"), keyPath, dir, nil, now, log); err == nil {
		t.Fatal("missing configured certificate accepted")
	}
}
