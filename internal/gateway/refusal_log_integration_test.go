package gateway_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// pastGraceRefused is the control plane's line for a refusal of a
// credential expired beyond the renewal grace.
const pastGraceRefused = "refused a credential expired beyond the renewal grace"

// textLinesWith returns every line of a text log stream that contains msg.
func (l *logLines) textLinesWith(msg string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if strings.Contains(line, msg) {
			out = append(out, line)
		}
	}
	return out
}

// TestPastGraceRefusalIsLogged is issue #221: a credential expired beyond
// the renewal grace is refused at every door, and its agent retries
// indefinitely, so the control plane's log records each refusal with the
// workload, the door, and the expiry, as it records a retired identity's.
// The transient refusal within the grace, which renewal ends, is not
// logged.
func TestPastGraceRefusalIsLogged(t *testing.T) {
	st := storetest.Open(t)
	logs := &logLines{}
	// A text handler, which writes the workload id as its UUID.
	h := newAgedHarness(t, st, func(d *gateway.Deps) { d.Log = slog.New(slog.NewTextHandler(logs, nil)) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	plain, _, err := h.service.MintToken(ctx, "past-grace", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	agent := enrollAgent(t, h, plain, "past-grace-1", "10.9.1.1/24")
	key, err := agent.cred.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	_, trust, err := agent.cred.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Within the grace, a sync stream is refused until the agent renews:
	// transient, and not logged.
	inGrace := h.issueAt(t, key, agent.id, time.Now().Add(-time.Hour))
	wantRefusal(t, "sync within the grace", refusedSync(ctx, t, h, &inGrace), "may only renew")
	if got := logs.textLinesWith(pastGraceRefused); len(got) != 0 {
		t.Fatalf("a refusal within the grace was logged as beyond it: %q", got)
	}

	// Beyond the grace: renewal and the sync stream are refused, and each
	// refusal is logged.
	notAfter := time.Now().Add(-(h.service.Grace() + time.Hour)).Truncate(time.Second)
	stale := h.issueAt(t, key, agent.id, notAfter)
	_, err = credential.Renew(ctx, key, credential.RenewOptions{Server: h.addr, Credential: stale, Trust: trust})
	wantRefusal(t, "renewal beyond the grace", err, enroll.ReenrollRequired)
	wantRefusal(t, "sync beyond the grace", refusedSync(ctx, t, h, &stale), enroll.ReenrollRequired)

	lines := logs.textLinesWith(pastGraceRefused)
	if len(lines) != 2 {
		t.Fatalf("logged %d past-grace refusals, want one per door (renewal, sync): %q", len(lines), lines)
	}
	doors := []string{
		innerwallv1.AgentService_RenewCredential_FullMethodName,
		innerwallv1.AgentService_Sync_FullMethodName,
	}
	for i, line := range lines {
		for _, want := range []string{
			"level=WARN",
			"workload_id=" + agent.id.String(),
			"door=" + doors[i],
			// The certificate's expiry, in UTC, to the second; the
			// handler adds milliseconds and the zone.
			"expired_at=" + notAfter.UTC().Format("2006-01-02T15:04:05"),
		} {
			if !strings.Contains(line, want) {
				t.Errorf("refusal line %d %q does not carry %q", i, line, want)
			}
		}
	}
}
