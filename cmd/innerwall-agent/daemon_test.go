package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce/nft"
	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/enroll/enrolltest"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// recordingRunner stands in for nft: it records every script and reports
// the owned table present.
type recordingRunner struct {
	mu      sync.Mutex
	scripts []string
}

func (r *recordingRunner) Apply(_ context.Context, script string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts = append(r.scripts, script)
	return nil
}

func (r *recordingRunner) List(context.Context, string) (string, error) { return "", nil }

func (r *recordingRunner) Tables(context.Context) ([]string, error) {
	return []string{nft.DefaultTable}, nil
}

func (r *recordingRunner) applied() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.scripts...)
}

// idleSource observes nothing until ctx ends.
type idleSource struct{}

func (idleSource) Run(ctx context.Context, _ func(collect.Observation)) error {
	<-ctx.Done()
	return nil
}

// expiredCredential signs a credential for key and id with the root in
// caDir that expired a minute ago: what a daemon finds on disk after a
// control-plane outage spanned its renewal window.
func expiredCredential(t *testing.T, caDir string, pub any, id identity.WorkloadID) []byte {
	t.Helper()
	rootKeyPEM, err := os.ReadFile(filepath.Join(caDir, fileca.KeyFile)) //nolint:gosec // the test's own temporary directory
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(rootKeyPEM)
	rootKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	rootPEM, err := os.ReadFile(filepath.Join(caDir, fileca.CertFile)) //nolint:gosec // the test's own temporary directory
	if err != nil {
		t.Fatal(err)
	}
	block, _ = pem.Decode(rootPEM)
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := ca.LeafTemplate(id, time.Now().Add(-3*time.Minute), 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, root, pub, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	return ca.EncodeCertificatePEM(der)
}

// TestDaemonStartsWithExpiredCredential restarts the daemon over a state
// directory whose credential expired while the control plane was away
// (issue #93): it re-applies the persisted policy before dialing, stays
// up, renews on its first attempt (the control plane accepts the expired
// credential for renewal within its grace, and records the renewal as
// late), and runs on the fresh credential from then on.
func TestDaemonStartsWithExpiredCredential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	// The control plane, with a count of the renewal calls it receives.
	caDir := t.TempDir()
	authority, err := fileca.Init(caDir, fileca.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := authority.Bundle(ctx)
	certPEM, keyPEM, err := authority.IssueServerCertificate([]string{"127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg, err := gateway.ServerTLSConfig(serverCert, bundle)
	if err != nil {
		t.Fatal(err)
	}
	st := enrolltest.NewMemStore()
	svc := &enroll.Service{Store: st, Authority: authority, LeafTTL: time.Hour}
	var renewCalls atomic.Int32
	count := grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod == innerwallv1.AgentService_RenewCredential_FullMethodName {
			renewCalls.Add(1)
		}
		return handler(ctx, req)
	})
	srv := gateway.NewGRPCServer(tlsCfg, gateway.New(gateway.Deps{Enroll: svc, Log: quiet}), count)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	// An enrolled workload whose stored credential has since expired, and
	// the policy it last acknowledged, persisted.
	plain, _, err := svc.MintToken(ctx, "outage", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	key, err := credential.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	res, err := credential.Enroll(ctx, key, credential.EnrollOptions{Server: lis.Addr().String(), Token: plain, BootstrapCA: bundle})
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	store := credential.Store{Dir: stateDir}
	if err := store.Save(key, expiredCredential(t, caDir, &key.PublicKey, res.WorkloadID), bundle); err != nil {
		t.Fatal(err)
	}
	persisted := &innerwallv1.WorkloadPolicy{Version: 3, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION}
	if err := nft.New(nft.Config{Runner: &recordingRunner{}, StateDir: stateDir, Log: quiet}).Apply(ctx, persisted); err != nil {
		t.Fatal(err)
	}

	runner := &recordingRunner{}
	dctx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() {
		done <- daemon(dctx, daemonOptions{
			server: lis.Addr().String(), stateDir: stateDir, bufferRecords: 100, windowKeys: 100, dumpMax: 100,
			table: nft.DefaultTable, nflogGroup: nft.DefaultNflogGroup, log: quiet, runner: runner, sources: idleSource{},
		})
	}()

	// Renewed: the stored credential is valid again, for the same identity.
	deadline := time.After(15 * time.Second)
	for {
		cred, _, err := store.Load()
		if err == nil && time.Now().Before(cred.Leaf.NotAfter) {
			if got, _ := identity.FromCertificate(cred.Leaf); got != res.WorkloadID {
				t.Fatalf("renewed identity %v, want %v", got, res.WorkloadID)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("daemon exited with an expired credential: %v", err)
		case <-deadline:
			t.Fatal("the daemon never renewed its expired credential")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if n := renewCalls.Load(); n != 1 {
		t.Fatalf("renewal calls = %d, want 1: the first attempt succeeds", n)
	}
	w, err := st.GetWorkload(ctx, res.WorkloadID)
	if err != nil {
		t.Fatal(err)
	}
	if w.LateRenewal == nil {
		t.Fatal("the late renewal was not recorded on the workload")
	}
	// Fail static held: the persisted policy went back into the kernel,
	// as one full rendering of the owned table, before anything else.
	scripts := runner.applied()
	if len(scripts) == 0 || !strings.Contains(scripts[0], "table inet "+nft.DefaultTable) {
		t.Fatalf("persisted policy not re-applied at start; scripts = %q", scripts)
	}

	select {
	case err := <-done:
		t.Fatalf("daemon exited after renewing: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	stop()
	if err := <-done; err != nil {
		t.Fatalf("daemon = %v, want nil on shutdown", err)
	}
}
