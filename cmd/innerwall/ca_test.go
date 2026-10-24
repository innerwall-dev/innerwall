package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// recordingIssuer is a ServerIssuer that records each issue and passes it
// to the authority it wraps, or fails when fail is set.
type recordingIssuer struct {
	inner ca.ServerIssuer
	fail  error
	hosts [][]string
	certs [][]byte
}

func (r *recordingIssuer) IssueServerCertificate(ctx context.Context, hosts []string, ttl time.Duration) ([]byte, []byte, error) {
	r.hosts = append(r.hosts, hosts)
	if r.fail != nil {
		return nil, nil, r.fail
	}
	certPEM, keyPEM, err := r.inner.IssueServerCertificate(ctx, hosts, ttl)
	r.certs = append(r.certs, certPEM)
	return certPEM, keyPEM, err
}

// recordingBackend composes the file-backed authority with every server
// certificate issued through issuer, which is what the commands under test
// must use: they reach the authority only through the backend.
func recordingBackend(issuer *recordingIssuer) authorityBackend {
	wrap := func(a signingAuthority, err error) (signingAuthority, error) {
		if err != nil {
			return a, err
		}
		issuer.inner = a.Server
		return signingAuthority{Authority: a.Authority, Server: issuer}, nil
	}
	return authorityBackend{
		Init: func(dir string, rootTTL time.Duration) (signingAuthority, error) {
			return wrap(fileBackend.Init(dir, rootTTL))
		},
		Open: func(dir string) (signingAuthority, error) { return wrap(fileBackend.Open(dir)) },
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test temp directory
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func parsePEMCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block")
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// snapshot reads every file in dir by name.
func snapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		out[e.Name()] = readFile(t, filepath.Join(dir, e.Name()))
	}
	return out
}

func newAuthorityDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ca")
	if err := initAuthority(context.Background(), fileBackend, dir, []string{"localhost", "127.0.0.1"}, fileca.DefaultRootTTL, defaultServerTTL, discard); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestIssueServerChainsToTheSameRoot is the property the command exists
// for: the new certificate names exactly the requested hosts, chains to the
// root agents already hold, and leaves that root, its key, and so the
// bundle exactly as they were.
func TestIssueServerChainsToTheSameRoot(t *testing.T) {
	ctx := context.Background()
	dir := newAuthorityDir(t)
	rootBefore := readFile(t, filepath.Join(dir, fileca.CertFile))
	keyBefore := readFile(t, filepath.Join(dir, fileca.KeyFile))
	before, err := fileBackend.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bundleBefore, err := before.Authority.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := issueServer(ctx, fileBackend, dir, []string{"cp.example.internal", " 192.0.2.10", ""}, time.Hour, discard); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(dir, fileca.CertFile)); !bytes.Equal(got, rootBefore) {
		t.Error("root certificate changed")
	}
	if got := readFile(t, filepath.Join(dir, fileca.KeyFile)); !bytes.Equal(got, keyBefore) {
		t.Error("root key changed")
	}
	after, err := fileBackend.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bundleAfter, err := after.Authority.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bundleAfter, bundleBefore) {
		t.Error("trust bundle changed")
	}

	cert := parsePEMCert(t, readFile(t, filepath.Join(dir, serverCertFile)))
	if !slices.Equal(cert.DNSNames, []string{"cp.example.internal"}) {
		t.Errorf("DNS SANs = %v, want exactly [cp.example.internal]", cert.DNSNames)
	}
	if len(cert.IPAddresses) != 1 || !cert.IPAddresses[0].Equal(net.ParseIP("192.0.2.10")) {
		t.Errorf("IP SANs = %v, want exactly [192.0.2.10]", cert.IPAddresses)
	}
	if len(cert.URIs) != 0 || len(cert.EmailAddresses) != 0 {
		t.Errorf("unexpected SANs: %v %v", cert.URIs, cert.EmailAddresses)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundleBefore) {
		t.Fatal("bundle holds no certificates")
	}
	for _, host := range []string{"cp.example.internal", "192.0.2.10"} {
		if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: host, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			t.Errorf("%s: does not verify against the bundle agents hold: %v", host, err)
		}
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost"}); err == nil {
		t.Error("the new certificate still names localhost, which was not requested")
	}
	if _, err := tls.LoadX509KeyPair(filepath.Join(dir, serverCertFile), filepath.Join(dir, serverKeyFile)); err != nil {
		t.Errorf("certificate and key do not form a pair: %v", err)
	}
	for name, want := range map[string]os.FileMode{serverKeyFile: 0o600, serverCertFile: 0o644} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %v, want %v", name, got, want)
		}
	}
	for name := range snapshot(t, dir) {
		if strings.Contains(name, ".new-") {
			t.Errorf("staging file %s left behind", name)
		}
	}
}

// TestIssueServerRefusesWithoutAuthority: the command issues from an
// existing authority and never creates one, so an empty or missing
// directory is refused, by name, and left as it was.
func TestIssueServerRefusesWithoutAuthority(t *testing.T) {
	ctx := context.Background()
	empty := t.TempDir()
	missing := filepath.Join(t.TempDir(), "absent")
	for _, dir := range []string{empty, missing} {
		err := issueServer(ctx, fileBackend, dir, []string{"cp.example.internal"}, time.Hour, discard)
		if err == nil {
			t.Fatalf("%s: issued without an authority", dir)
		}
		if !strings.Contains(err.Error(), "no signing authority in "+dir) || !strings.Contains(err.Error(), "ca init") {
			t.Errorf("%s: error does not say what is missing: %v", dir, err)
		}
	}
	if got := snapshot(t, empty); len(got) != 0 {
		t.Errorf("refusal wrote into the directory: %v", got)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("refusal created the directory: %v", err)
	}
}

// TestIssueServerRequiresHosts: a certificate for no host would leave every
// agent refusing the gateway, so the command refuses before touching the
// directory.
func TestIssueServerRequiresHosts(t *testing.T) {
	dir := newAuthorityDir(t)
	before := snapshot(t, dir)
	for _, hosts := range [][]string{nil, {""}, {" ", ""}} {
		if err := issueServer(context.Background(), fileBackend, dir, hosts, time.Hour, discard); err == nil || !strings.Contains(err.Error(), "--hosts") {
			t.Errorf("%q: %v, want a refusal naming --hosts", hosts, err)
		}
	}
	if after := snapshot(t, dir); !mapsEqual(before, after) {
		t.Error("a refused issue changed the directory")
	}
}

// TestIssueServerFailureLeavesThePreviousPair: the new pair replaces the old
// one only once it exists in full; an issue that fails part-way leaves the
// certificate and key serve loads exactly as they were, and no staging
// files beside them.
func TestIssueServerFailureLeavesThePreviousPair(t *testing.T) {
	ctx := context.Background()
	t.Run("the authority refuses", func(t *testing.T) {
		dir := newAuthorityDir(t)
		before := snapshot(t, dir)
		if err := issueServer(ctx, fileBackend, dir, []string{"cp.example.internal"}, 0, discard); err == nil {
			t.Fatal("a zero lifetime was issued")
		}
		if after := snapshot(t, dir); !mapsEqual(before, after) {
			t.Error("the previous server certificate or key changed")
		}
	})
	t.Run("the issuer fails", func(t *testing.T) {
		dir := newAuthorityDir(t)
		before := snapshot(t, dir)
		issuer := &recordingIssuer{fail: errors.New("signing backend unavailable")}
		err := issueServer(ctx, recordingBackend(issuer), dir, []string{"cp.example.internal"}, time.Hour, discard)
		if err == nil || !strings.Contains(err.Error(), "signing backend unavailable") {
			t.Fatalf("got %v, want the issuer's error", err)
		}
		if after := snapshot(t, dir); !mapsEqual(before, after) {
			t.Error("the previous server certificate or key changed")
		}
	})
	t.Run("the replacement cannot be written", func(t *testing.T) {
		dir := newAuthorityDir(t)
		before := snapshot(t, dir)
		// The first file is staged; the second cannot be, so neither is
		// renamed into place.
		err := replaceFiles(dir, []stagedFile{
			{name: serverKeyFile, data: []byte("new key"), mode: 0o600},
			{name: filepath.Join("absent", serverCertFile), data: []byte("new cert"), mode: 0o644},
		})
		if err == nil {
			t.Fatal("staging into a missing directory succeeded")
		}
		if after := snapshot(t, dir); !mapsEqual(before, after) {
			t.Error("a failed replacement changed the directory")
		}
	})
}

// TestServeIssuesThroughServerIssuer: serve's init path reaches the
// authority only through the composition it is handed. With --init-ca it
// issues the gateway's certificate through the backend's ServerIssuer and
// presents that certificate; on a later start the existing pair is kept,
// and after `ca issue-server` a restart presents the new one.
func TestServeIssuesThroughServerIssuer(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "ca")
	cfg := gatewayTLS{CADir: dir, InitCA: true, Hosts: []string{"localhost", " 127.0.0.1"}}

	issuer := &recordingIssuer{}
	authority, tlsCfg, err := gatewayCredentials(ctx, recordingBackend(issuer), cfg, discard)
	if err != nil {
		t.Fatal(err)
	}
	if authority == nil {
		t.Fatal("no authority for enrollment")
	}
	if len(issuer.hosts) != 1 || !slices.Equal(issuer.hosts[0], []string{"localhost", "127.0.0.1"}) {
		t.Fatalf("issues through the ServerIssuer = %v, want one for [localhost 127.0.0.1]", issuer.hosts)
	}
	first := presented(t, tlsCfg)
	if !bytes.Equal(first.Raw, parsePEMCert(t, issuer.certs[0]).Raw) {
		t.Fatal("serve presents a certificate other than the one its ServerIssuer issued")
	}

	// A later start with the same flags creates nothing and keeps the pair.
	again := &recordingIssuer{}
	_, tlsCfg, err = gatewayCredentials(ctx, recordingBackend(again), cfg, discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.hosts) != 0 {
		t.Fatalf("an existing authority was issued for again: %v", again.hosts)
	}
	if !bytes.Equal(presented(t, tlsCfg).Raw, first.Raw) {
		t.Fatal("a later start replaced the existing certificate")
	}

	// Reissue for a new host list; the restart presents it.
	reissue := &recordingIssuer{}
	if err := issueServer(ctx, recordingBackend(reissue), dir, []string{"cp.example.internal"}, time.Hour, discard); err != nil {
		t.Fatal(err)
	}
	if len(reissue.hosts) != 1 {
		t.Fatalf("issue-server did not issue through the ServerIssuer: %v", reissue.hosts)
	}
	_, tlsCfg, err = gatewayCredentials(ctx, recordingBackend(&recordingIssuer{}), cfg, discard)
	if err != nil {
		t.Fatal(err)
	}
	if got := presented(t, tlsCfg); !slices.Equal(got.DNSNames, []string{"cp.example.internal"}) {
		t.Fatalf("after a restart serve presents %v, want the reissued certificate", got.DNSNames)
	}
}

func presented(t *testing.T, cfg *tls.Config) *x509.Certificate {
	t.Helper()
	if len(cfg.Certificates) != 1 || len(cfg.Certificates[0].Certificate) == 0 {
		t.Fatal("the gateway presents no certificate")
	}
	cert, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func mapsEqual(a, b map[string][]byte) bool { return maps.EqualFunc(a, b, bytes.Equal) }
