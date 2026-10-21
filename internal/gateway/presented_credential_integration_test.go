package gateway_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// logLines collects a JSON log stream, for asserting on what a line
// carried.
type logLines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// all returns every line with message msg, decoded.
func (l *logLines) all(t *testing.T, msg string) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(l.buf.Bytes()))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}
		if line["msg"] == msg {
			out = append(out, line)
		}
	}
	return out
}

// leafOf returns the certificate the agent will present next.
func leafOf(t *testing.T, a *testAgent) *x509.Certificate {
	t.Helper()
	cred, _, err := a.cred.Load()
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cred.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

// TestStreamOpenRecordsThePresentedCredential runs issue #112's case over
// Postgres and a real TLS listener: the stream's open stamps the serial
// and expiry of the certificate it presented and logs them on "sync
// stream opened"; a renewal while that stream runs moves the issued
// credential and leaves the presented one, so the workload read reports
// the credential in use apart from the one last issued; the stream ending
// leaves the stamp with the sync state offline; and the next stream, on
// the renewed credential, moves it.
func TestStreamOpenRecordsThePresentedCredential(t *testing.T) {
	st := storetest.Open(t)
	logs := &logLines{}
	h := newHarness(t, st, func(d *gateway.Deps) { d.Log = slog.New(slog.NewJSONHandler(logs, nil)) })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}

	token, _, err := h.service.MintToken(ctx, "web", []enroll.Label{{Key: "role", Value: "web"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	agent := enrollAgent(t, h, token, "web-1", "10.0.0.10")
	f := facts("web-1", "10.0.0.10")
	if w, err := st.LookupWorkload(ctx, agent.id); err != nil || w.PresentedCredential != nil {
		t.Fatalf("before any stream: presented = %+v, err = %v", w.PresentedCredential, err)
	}

	// The open stamps the enrolled credential, which is also the one
	// last issued, and logs it.
	first := leafOf(t, agent)
	firstSerial := enroll.CredentialSerial(first)
	agent.connect(0, f)
	agent.expectHelloAck()
	agent.expectSnapshot()
	w := waitFor(t, st, agent.id, "the presented credential", func(w *registry.Workload) bool { return w.PresentedCredential != nil })
	if p := w.PresentedCredential; p.Serial != firstSerial || !p.ExpiresAt.Equal(first.NotAfter) || w.CredentialSerial != firstSerial {
		t.Fatalf("at open: presented = %+v, issued %q; want %q expiring %v", p, w.CredentialSerial, firstSerial, first.NotAfter)
	}
	lines := logs.all(t, "sync stream opened")
	if len(lines) != 1 || lines[0]["credential_serial"] != firstSerial || lines[0]["credential_expires_at"] != first.NotAfter.Format(time.RFC3339Nano) {
		t.Fatalf("sync stream opened lines = %v, want serial %s expiring %v", lines, firstSerial, first.NotAfter)
	}

	// A renewal while the stream holds the old credential: the issued
	// credential moves, the stream and the stamp stay on the old one.
	renewCredential(t, agent)
	renewedSerial := enroll.CredentialSerial(leafOf(t, agent))
	if renewedSerial == firstSerial {
		t.Fatal("renewal issued the same serial")
	}
	got, err := reads.GetWorkload(ctx, agent.id)
	if err != nil {
		t.Fatal(err)
	}
	c := got.Health.Credential
	if c.Serial != renewedSerial || c.LastPresented == nil || c.LastPresented.Serial != firstSerial || !c.LastPresented.ExpiresAt.Equal(first.NotAfter) {
		t.Fatalf("read after renewal: issued %q, presented %+v; want issued %q, presented %q", c.Serial, c.LastPresented, renewedSerial, firstSerial)
	}
	if got.Sync.State == innerwallv1.SyncState_SYNC_STATE_OFFLINE {
		t.Fatal("the stream holding the old credential reads offline")
	}
	agent.expectNothing(200 * time.Millisecond)

	// The stream ends: the stamp stays, as what was presented last.
	agent.disconnect()
	w = waitFor(t, st, agent.id, "offline", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE })
	if p := w.PresentedCredential; p == nil || p.Serial != firstSerial {
		t.Fatalf("after the stream ended: presented = %+v, want %q", p, firstSerial)
	}

	// The next stream presents the renewed credential.
	agent.connect(0, f)
	agent.expectHelloAck()
	agent.expectSnapshot()
	w = waitFor(t, st, agent.id, "the renewed credential presented", func(w *registry.Workload) bool {
		return w.PresentedCredential != nil && w.PresentedCredential.Serial == renewedSerial
	})
	if w.CredentialSerial != renewedSerial || !w.PresentedCredential.ExpiresAt.Equal(w.CredentialExpiresAt) {
		t.Fatalf("on the renewed stream: presented = %+v, issued %q expiring %v", w.PresentedCredential, w.CredentialSerial, w.CredentialExpiresAt)
	}
	if lines := logs.all(t, "sync stream opened"); len(lines) != 2 || lines[1]["credential_serial"] != renewedSerial {
		t.Fatalf("sync stream opened lines = %v, want the second on %s", lines, renewedSerial)
	}
	agent.disconnect()
}
