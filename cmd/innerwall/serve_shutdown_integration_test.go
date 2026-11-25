package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// logBuffer collects a log written from many goroutines.
type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestServeShutdownMarksStreamsOffline stops the control plane, as
// `innerwall serve` builds it, while agents hold their sync streams. Every
// stream's end must be recorded before the process lets go of the
// database: by the time serve returns, each workload reads offline with
// a last-seen instant from the shutdown, and the log carries no failed
// offline mark. The store is given fewer connections than there are
// streams, so the marks queue for a connection as the streams end, which
// is where a store closed under them refuses them.
func TestServeShutdownMarksStreamsOffline(t *testing.T) {
	const agents = 4
	st := storetest.Open(t)
	dbURL, err := url.Parse(os.Getenv(storetest.EnvDatabaseURL))
	if err != nil {
		t.Fatal(err)
	}
	q := dbURL.Query()
	q.Set("pool_max_conns", "2")
	dbURL.RawQuery = q.Encode()
	caDir := t.TempDir()
	gateway := reservePort(t)
	flags := []string{"--operator-listen", reservePort(t), "--ca-dir", caDir, "--init-ca", "--database-url", dbURL.String()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	logs := &logBuffer{}
	stop := startServeLogging(t, gateway, logs, flags...)
	stopped := false
	defer func() {
		if !stopped {
			stop()
		}
	}()
	plain, _, err := (&enroll.Service{Store: st}).MintToken(ctx, "shutdown", nil, 0)
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

	ids := make([]identity.WorkloadID, 0, agents)
	daemonCtx, stopDaemons := context.WithCancel(ctx)
	var daemons sync.WaitGroup
	defer func() { stopDaemons(); daemons.Wait() }()
	for i := range agents {
		key, err := credential.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		facts := &innerwallv1.HostFacts{Hostname: fmt.Sprintf("shutdown-%d", i)}
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
		daemon := agentsync.New(agentsync.Config{
			Server:      gateway,
			Holder:      holder,
			Store:       &enforce.MemoryStore{},
			Facts:       func() *innerwallv1.HostFacts { return facts },
			BackoffBase: 50 * time.Millisecond,
			BackoffCap:  250 * time.Millisecond,
			Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		daemons.Add(1)
		go func() { defer daemons.Done(); _ = daemon.Run(daemonCtx) }()
		ids = append(ids, enrolled.WorkloadID)
	}

	// Every stream is open and settled: its snapshot applied and
	// acknowledged, and the instant it was sent recorded.
	for _, id := range ids {
		deadline := time.Now().Add(15 * time.Second)
		for {
			w, err := st.LookupWorkload(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.LastSnapshotSentAt != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("workload %v did not sync; workload = %+v", id, w)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// Stop the control plane with the agents still connected.
	began := time.Now()
	stop()
	stopped = true
	returned := time.Now()

	for _, id := range ids {
		w, err := st.LookupWorkload(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if w.SyncState != innerwallv1.SyncState_SYNC_STATE_OFFLINE {
			t.Errorf("workload %v: sync state after shutdown = %v, want offline", id, w.SyncState)
			continue
		}
		if w.LastSeenAt == nil || w.LastSeenAt.Before(began) || w.LastSeenAt.After(returned) {
			t.Errorf("workload %v: last seen %v, want the shutdown's offline mark, between %v and %v", id, w.LastSeenAt, began, returned)
		}
	}
	out := logs.String()
	if !strings.Contains(out, "control plane serving") {
		t.Fatalf("the control plane's log was not captured:\n%s", out)
	}
	for _, bad := range []string{"marking workload offline", "closed pool"} {
		if strings.Contains(out, bad) {
			t.Errorf("the log carries %q:\n%s", bad, out)
		}
	}
}
