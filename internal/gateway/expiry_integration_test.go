package gateway_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// expiryLead is the lead the expiry tests run with: long enough to tell
// the directive from the end, short enough to keep the tests quick.
const expiryLead = 2 * time.Second

// expiringAt stores in a's credential store a credential for a's identity
// and key that expires at notAfter, signed by the harness's own root, so
// the next stream a opens is bounded by it. notAfter is truncated to the
// second a certificate can carry; the truncated instant is returned.
func expiringAt(t *testing.T, a *testAgent, notAfter time.Time) time.Time {
	t.Helper()
	notAfter = notAfter.Truncate(time.Second)
	key, err := a.cred.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	cert := a.h.issueAt(t, key, a.id, notAfter)
	if err := a.cred.ReplaceCertificate(ca.EncodeCertificatePEM(cert.Certificate[0]), a.h.bundle); err != nil {
		t.Fatal(err)
	}
	return notAfter
}

// endedAtExpiry waits for a's stream to end, failing if a directive
// arrives first, and returns why it ended.
func endedAtExpiry(t *testing.T, a *testAgent) error {
	t.Helper()
	for {
		select {
		case m := <-a.msgs:
			if m.GetDirective() != nil {
				t.Fatalf("a directive arrived where none should: %v", m)
			}
		case err := <-a.errs:
			return err
		case <-time.After(15 * time.Second):
			t.Fatal("the stream did not end")
			return nil
		}
	}
}

// wantExpiryEnd checks err is the end of a stream whose credential expired
// at notAfter: Unauthenticated, naming the expiry and its instant, and
// never the opaque refusal a retired identity receives.
func wantExpiryEnd(t *testing.T, what string, err error, notAfter time.Time) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Fatalf("%s: err = %v, want Unauthenticated", what, err)
	}
	want := enroll.ErrCredentialExpired.Error() + ": it expired at " + notAfter.UTC().Format(time.RFC3339)
	if !strings.HasPrefix(st.Message(), want) || st.Message() == refusedMessage {
		t.Fatalf("%s: message %q, want it to begin %q", what, st.Message(), want)
	}
}

// TestStreamAuthorizationEndsWithItsCredential is a credential expiring
// mid-stream against Postgres and a real TLS listener (issue #105,
// ADR-0016 as amended). The stream is directed to reconnect the lead
// before its credential's expiry, and nothing earlier; an agent that stays
// on it is ended at the expiry with a refusal naming it, and its expired
// credential then opens nothing but may renew. A stream opened within the
// lead is not directed, since its agent has just dialed with the
// credential it holds, and is ended at the expiry. An agent that renews
// when directed and opens a new stream on the fresh credential is served
// on past the old expiry, without an error.
func TestStreamAuthorizationEndsWithItsCredential(t *testing.T) {
	gateway.SetCredentialEndLead(t, expiryLead)
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tok, _, err := h.service.MintToken(ctx, "expiry", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	agent := enrollAgent(t, h, tok, "exp-1", "10.11.0.1/24")
	f := facts("exp-1", "10.11.0.1/24")

	// The agent stays on the old connection: directed at the lead, ended
	// at the expiry.
	notAfter := expiringAt(t, agent, time.Now().Add(6*time.Second))
	agent.connect(0, f)
	agent.expectHelloAck()
	agent.expectSnapshot()
	expectReconnect(t, agent, "the lead before expiry")
	if now := time.Now(); now.Before(notAfter.Add(-expiryLead)) || !now.Before(notAfter) {
		t.Fatalf("directed at %v, want within [%v, %v)", now, notAfter.Add(-expiryLead), notAfter)
	}
	err = endedAtExpiry(t, agent)
	if now := time.Now(); now.Before(notAfter) {
		t.Fatalf("ended at %v, before the credential expired at %v", now, notAfter)
	}
	wantExpiryEnd(t, "the stream at expiry", err, notAfter)
	cred, trust, err := agent.cred.Load()
	if err != nil {
		t.Fatal(err)
	}
	wantRefusal(t, "a new stream on the expired credential", refusedSync(ctx, t, h, &cred), "may only renew")

	// Opened within the lead: no directive, ended at the expiry.
	notAfter = expiringAt(t, agent, time.Now().Add(expiryLead))
	if time.Until(notAfter) <= 0 {
		notAfter = expiringAt(t, agent, time.Now().Add(expiryLead+time.Second))
	}
	agent.connect(0, f)
	agent.expectHelloAck()
	agent.expectSnapshot()
	wantExpiryEnd(t, "a stream opened within the lead", endedAtExpiry(t, agent), notAfter)

	// The expired credential renews within the grace, which is the
	// agent's way back; the renewed one opens a stream again.
	key, err := agent.cred.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	cred, _, err = agent.cred.Load()
	if err != nil {
		t.Fatal(err)
	}
	res, err := credential.Renew(ctx, key, credential.RenewOptions{Server: h.addr, Credential: cred, Trust: trust})
	if err != nil {
		t.Fatalf("renewing the credential a stream ended with: %v", err)
	}
	if err := agent.cred.ReplaceCertificate(res.CertificatePEM, res.BundlePEM); err != nil {
		t.Fatal(err)
	}
	agent.connect(0, f)
	agent.expectHelloAck()
	agent.expectSnapshot()
	agent.disconnect()

	// The seamless path: renewed when directed, redialed on the fresh
	// credential before the old one expired, and served past it.
	notAfter = expiringAt(t, agent, time.Now().Add(5*time.Second))
	agent.connect(0, f)
	agent.expectHelloAck()
	agent.expectSnapshot()
	expectReconnect(t, agent, "the lead before expiry, with renewal to follow")
	renewCredential(t, agent)
	agent.disconnect()
	agent.connect(0, f)
	agent.expectHelloAck()
	agent.expectSnapshot()
	if !time.Now().Before(notAfter) {
		t.Fatalf("the renewed stream opened after the old credential expired at %v", notAfter)
	}
	select {
	case m := <-agent.msgs:
		t.Fatalf("the renewed stream received %v", m)
	case err := <-agent.errs:
		t.Fatalf("the renewed stream ended at the old credential's expiry: %v", err)
	case <-time.After(time.Until(notAfter) + 2*time.Second):
	}
	agent.disconnect()
}

// TestExpiryAndRetirementEndStreamsDifferently ends two live streams, one
// at its credential's expiry and one by retirement, and checks the two
// refusals differ exactly as ADR-0016 and ADR-0023 record: expiry is a
// validity refusal and names its reason and instant; retirement is the
// fixed opaque refusal and nothing more. Both are Unauthenticated, and
// beyond the grace, renewal is refused naming re-enrollment.
func TestExpiryAndRetirementEndStreamsDifferently(t *testing.T) {
	gateway.SetCredentialEndLead(t, expiryLead)
	st := storetest.Open(t)
	h := newAgedHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tok, _, err := h.service.MintToken(ctx, "ends", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	lapsing := enrollAgent(t, h, tok, "lapse-1", "10.12.0.1/24")
	retiring := enrollAgent(t, h, tok, "retire-1", "10.12.0.2/24")

	notAfter := expiringAt(t, lapsing, time.Now().Add(4*time.Second))
	lapsing.connect(0, facts("lapse-1", "10.12.0.1/24"))
	retiring.connect(0, facts("retire-1", "10.12.0.2/24"))
	for _, a := range []*testAgent{lapsing, retiring} {
		a.expectHelloAck()
		a.expectSnapshot()
	}
	if _, err := fleetOver(st, h, h.gw).Retire(operatorContext(ctx), retiring.id); err != nil {
		t.Fatal(err)
	}
	retiredEnd := streamEnded(t, retiring)
	expiredEnd := streamEnded(t, lapsing)

	wantOpaqueRefusal(t, "the retired workload's stream", retiredEnd)
	wantExpiryEnd(t, "the lapsed credential's stream", expiredEnd, notAfter)
	if status.Convert(retiredEnd).Message() == status.Convert(expiredEnd).Message() {
		t.Fatal("retirement and expiry ended their streams with the same message")
	}

	// Beyond the grace, the agent's renewal is refused with its reason.
	key, err := lapsing.cred.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	_, trust, err := lapsing.cred.Load()
	if err != nil {
		t.Fatal(err)
	}
	stale := h.issueAt(t, key, lapsing.id, time.Now().Add(-(h.service.Grace() + time.Hour)))
	_, err = credential.Renew(ctx, key, credential.RenewOptions{Server: h.addr, Credential: stale, Trust: trust})
	wantRefusal(t, "renewal beyond the grace", err, enroll.ReenrollRequired)
}
