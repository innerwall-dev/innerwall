package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// reservePort returns a loopback address free at the time of the call.
// The control plane is restarted on the same address, as agents are
// configured with one.
func reservePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// startServe runs the control plane exactly as `innerwall serve` does,
// with the given flags, and returns once the agent gateway accepts
// connections. The returned function stops it and waits for it to return.
func startServe(t *testing.T, gateway string, args ...string) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServe(ctx, append([]string{"--listen", gateway}, args...)) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := net.Dial("tcp", gateway)
		if err == nil {
			_ = c.Close()
			break
		}
		select {
		case err := <-done:
			cancel()
			t.Fatalf("serve returned before listening: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("serve did not start listening")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	}
}

// TestServeRefusesExpiredCredentialAfterRestart is the renewal grace
// (ADR-0016 as amended) through the control plane as `innerwall serve`
// builds it, in the shape an outage takes: a workload renews on a live
// stream, the control plane goes away until that credential has lapsed,
// and comes back while the agent's sync daemon is redialing. The sync
// stream and flow reports must be refused with the validity reason, a
// renewal must then succeed under the grace and be recorded as late, and
// the stream must resume on the renewed credential, in that order.
func TestServeRefusesExpiredCredentialAfterRestart(t *testing.T) {
	st := storetest.Open(t)
	dbURL := os.Getenv(storetest.EnvDatabaseURL)
	caDir := t.TempDir()
	gateway := reservePort(t)
	flags := []string{"--operator-listen", reservePort(t), "--ca-dir", caDir, "--init-ca", "--database-url", dbURL, "--leaf-ttl", "6s"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stop := startServe(t, gateway, flags...)
	plain, _, err := (&enroll.Service{Store: st}).MintToken(ctx, "grace", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := fileca.Open(caDir)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := authority.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key, err := credential.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	facts := &innerwallv1.HostFacts{Hostname: "grace-serve-1"}
	enrolled, err := credential.Enroll(ctx, key, credential.EnrollOptions{Server: gateway, Token: plain, BootstrapCA: bootstrap, Facts: facts})
	if err != nil {
		t.Fatal(err)
	}
	creds := credential.Store{Dir: t.TempDir()}
	if err := creds.Save(key, enrolled.CertificatePEM, enrolled.BundlePEM); err != nil {
		t.Fatal(err)
	}
	holder, err := credential.LoadHolder(creds)
	if err != nil {
		t.Fatal(err)
	}

	established := make(chan struct{}, 16)
	daemon := agentsync.New(agentsync.Config{
		Server:       gateway,
		Holder:       holder,
		Store:        &enforce.MemoryStore{},
		Facts:        func() *innerwallv1.HostFacts { return facts },
		BackoffBase:  50 * time.Millisecond,
		BackoffCap:   250 * time.Millisecond,
		OnSyncConfig: func(*innerwallv1.SyncConfig) { established <- struct{}{} },
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	daemonCtx, stopDaemon := context.WithCancel(ctx)
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Run(daemonCtx) }()
	defer func() { stopDaemon(); <-daemonDone }()
	awaitEstablished := func(what string) {
		t.Helper()
		select {
		case <-established:
		case <-time.After(15 * time.Second):
			t.Fatalf("%s: the sync stream was not established", what)
		}
	}
	awaitEstablished("enrolled credential")

	// Renew on time while the stream is live, as the renewer does; the
	// credential that lapses below is a renewed one.
	cur, trust := holder.Current()
	renewed, err := credential.Renew(ctx, key, credential.RenewOptions{Server: gateway, Credential: cur, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Replace(renewed.CertificatePEM, renewed.BundlePEM); err != nil {
		t.Fatal(err)
	}
	lapsing := holder.Leaf()

	// The outage: the control plane stops and stays away until the
	// renewed credential has expired. The daemon keeps redialing.
	stop()
	for time.Now().Before(lapsing.NotAfter.Add(time.Second)) {
		time.Sleep(100 * time.Millisecond)
	}
	for len(established) > 0 {
		<-established
	}
	stop = startServe(t, gateway, flags...)
	defer func() { stop() }()

	// Refused on the sync stream and on flow reports, with the reason.
	client, closer, err := agentsync.DialGRPC(ctx, gateway, holder)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closer.Close() }()
	stream, err := client.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Hello{Hello: &innerwallv1.Hello{Facts: facts}}})
	_, err = stream.Recv()
	wantExpiredRefusal(t, "sync after the restart", err)
	flows, err := client.ReportFlows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = flows.CloseAndRecv()
	wantExpiredRefusal(t, "flow report after the restart", err)
	// The daemon has been redialing the whole time; give it a few more
	// attempts against the returned control plane.
	select {
	case <-established:
		t.Fatalf("the agent's sync stream was established on a credential that expired at %s", lapsing.NotAfter.UTC().Format(time.RFC3339))
	case <-time.After(time.Second):
	}

	// The next renewal succeeds under the grace and is recorded as late.
	cur, trust = holder.Current()
	late, err := credential.Renew(ctx, key, credential.RenewOptions{Server: gateway, Credential: cur, Trust: trust})
	if err != nil {
		t.Fatalf("renewal within the grace: %v", err)
	}
	if late.WorkloadID != enrolled.WorkloadID {
		t.Fatalf("renewed identity = %v, want %v", late.WorkloadID, enrolled.WorkloadID)
	}
	w, err := st.LookupWorkload(ctx, enrolled.WorkloadID)
	if err != nil {
		t.Fatal(err)
	}
	if w.LateRenewal == nil || !w.LateRenewal.ExpiredAt.Equal(lapsing.NotAfter) {
		t.Fatalf("late renewal = %+v, want one for the credential that expired at %v", w.LateRenewal, lapsing.NotAfter)
	}

	// Everything resumes on the renewed credential.
	if err := holder.Replace(late.CertificatePEM, late.BundlePEM); err != nil {
		t.Fatal(err)
	}
	awaitEstablished("renewed credential")
}

func wantExpiredRefusal(t *testing.T, what string, err error) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Fatalf("%s: err = %v, want Unauthenticated", what, err)
	}
	if !strings.Contains(st.Message(), enroll.ErrCredentialExpired.Error()) || !strings.Contains(st.Message(), "may only renew") {
		t.Fatalf("%s: message %q does not give the validity reason", what, st.Message())
	}
}
