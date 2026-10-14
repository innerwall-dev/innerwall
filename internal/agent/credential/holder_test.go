package credential_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// issue signs a fresh certificate for key with the given lifetime.
func issue(t *testing.T, authority *fileca.Authority, key *ecdsa.PrivateKey, id identity.WorkloadID, ttl time.Duration) []byte {
	t.Helper()
	csr, err := credential.NewCSR(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := authority.Sign(context.Background(), csr, id, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return certPEM
}

func newHolder(t *testing.T, ttl time.Duration) (*credential.Holder, *fileca.Authority, identity.WorkloadID, credential.Store) {
	t.Helper()
	authority, err := fileca.Init(t.TempDir(), fileca.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := authority.Bundle(context.Background())
	key, _ := credential.GenerateKey()
	id, _ := identity.NewWorkloadID()
	store := credential.Store{Dir: t.TempDir()}
	if err := store.Save(key, issue(t, authority, key, id, ttl), bundle); err != nil {
		t.Fatal(err)
	}
	h, err := credential.LoadHolder(store)
	if err != nil {
		t.Fatal(err)
	}
	return h, authority, id, store
}

func TestRenewAtWindow(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // test
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lifetime := 24 * time.Hour
	notAfter := notBefore.Add(lifetime)
	earliest := notBefore.Add(time.Duration(float64(lifetime) * (credential.RenewFraction - credential.RenewJitterFraction)))
	latest := notBefore.Add(time.Duration(float64(lifetime) * credential.RenewFraction))
	for range 500 {
		at := credential.RenewAt(notBefore, notAfter, rng)
		if at.Before(earliest) || !at.Before(latest) {
			t.Fatalf("RenewAt = %v, want in [%v, %v)", at, earliest, latest)
		}
		// Always with more than a third of the lifetime left.
		if notAfter.Sub(at) < lifetime/3 {
			t.Fatalf("RenewAt = %v leaves %v, less than a third", at, notAfter.Sub(at))
		}
	}
}

// TestRenewerFiresAtThreshold drives the schedule with a fake clock: the
// first wait lands in the renewal window, the renewal swaps the credential
// in the holder, on disk, and in what the TLS configuration presents, a
// failure retries with bounded backoff, and expiry ends the loop.
func TestRenewerFiresAtThreshold(t *testing.T) {
	h, authority, id, store := newHolder(t, 24*time.Hour)
	leaf := h.Leaf()
	lifetime := leaf.NotAfter.Sub(leaf.NotBefore)

	var mu sync.Mutex
	now := leaf.NotBefore.Add(time.Hour)
	var waits []time.Duration
	after := func(d time.Duration) <-chan time.Time {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, d)
		if len(waits) > 2 {
			// The schedule after the successful renewal: hold there so
			// the test observes exactly one failure and one success.
			return make(chan time.Time)
		}
		now = now.Add(d)
		ch := make(chan time.Time, 1)
		ch <- now
		return ch
	}
	calls := 0
	fail := true
	renewals := make(chan *x509.Certificate, 4)
	r := &credential.Renewer{
		Holder:    h,
		Server:    "unused",
		RetryBase: time.Minute,
		RetryCap:  10 * time.Minute,
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return now
		},
		After: after,
		Renew: func(_ context.Context, key *ecdsa.PrivateKey, opts credential.RenewOptions) (*credential.RenewResult, error) {
			calls++
			if opts.Credential.Leaf == nil || opts.Credential.Leaf.SerialNumber.Cmp(h.Leaf().SerialNumber) != 0 {
				t.Errorf("renewal presented a credential other than the holder's")
			}
			if fail {
				fail = false
				return nil, errors.New("control plane unreachable")
			}
			// Issue a certificate that is already mostly spent, so the
			// next scheduled renewal falls after the fake clock's expiry
			// jump below.
			return &credential.RenewResult{WorkloadID: id, CertificatePEM: issue(t, authority, key, id, 24*time.Hour)}, nil
		},
		Renewed: func(c *x509.Certificate) { renewals <- c },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case renewed := <-renewals:
		if renewed.SerialNumber.Cmp(leaf.SerialNumber) == 0 {
			t.Fatal("renewal kept the serial")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no renewal")
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("renew calls = %d, want 2 (one failure, one success)", calls)
	}
	// First wait: from now (notBefore+1h) to a point in the renewal window.
	first := waits[0]
	lo := time.Duration(float64(lifetime)*(credential.RenewFraction-credential.RenewJitterFraction)) - time.Hour
	hi := time.Duration(float64(lifetime)*credential.RenewFraction) - time.Hour
	if first < lo || first >= hi {
		t.Fatalf("first wait %v not in [%v, %v)", first, lo, hi)
	}
	// Second wait: the retry backoff, bounded by base << 1.
	if waits[1] < 0 || waits[1] > 2*time.Minute {
		t.Fatalf("retry wait %v out of bounds", waits[1])
	}
	// The holder, the disk, and the TLS configuration all present the new
	// certificate.
	if h.Leaf().SerialNumber.Cmp(leaf.SerialNumber) == 0 {
		t.Fatal("holder still has the old certificate")
	}
	onDisk, _, err := store.Load()
	if err != nil || onDisk.Leaf.SerialNumber.Cmp(h.Leaf().SerialNumber) != 0 {
		t.Fatalf("disk holds %v (err %v), holder has %v", onDisk.Leaf, err, h.Leaf().SerialNumber)
	}
	cfg, err := h.TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	presented, err := cfg.GetClientCertificate(nil)
	if err != nil || presented.Leaf.SerialNumber.Cmp(h.Leaf().SerialNumber) != 0 {
		t.Fatalf("tls presents %v, want %v", presented.Leaf.SerialNumber, h.Leaf().SerialNumber)
	}
}

func TestRenewerRenewsImmediatelyWhenLittleRemains(t *testing.T) {
	// A three-second credential is backdated by ca.BackdateNotBefore, so
	// far more than two thirds of its lifetime is already spent: the
	// renewer must fire at once rather than schedule.
	h, authority, id, _ := newHolder(t, 3*time.Second)
	renewed := make(chan *x509.Certificate, 1)
	r := &credential.Renewer{
		Holder: h, Server: "unused",
		Renew: func(_ context.Context, key *ecdsa.PrivateKey, _ credential.RenewOptions) (*credential.RenewResult, error) {
			return &credential.RenewResult{WorkloadID: id, CertificatePEM: issue(t, authority, key, id, time.Hour)}, nil
		},
		Renewed: func(c *x509.Certificate) { renewed <- c },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()
	select {
	case c := <-renewed:
		if time.Until(c.NotAfter) < 30*time.Minute {
			t.Fatalf("renewed certificate expires %v", c.NotAfter)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("renewal did not fire immediately")
	}
	_ = ca.BackdateNotBefore
}

// fakeClocks drives a renewer without waiting: a wall clock that moves
// only when told, and a monotonic clock that every wait advances.
type fakeClocks struct {
	mu      sync.Mutex
	wall    time.Time
	elapsed time.Duration
	waits   []time.Duration
	// hold makes waits past this many block, so a test observes a fixed
	// number of attempts.
	hold int
	// wallMoves says whether waits advance the wall clock too; false is
	// a host whose wall clock is stuck or far behind.
	wallMoves bool
}

func (c *fakeClocks) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall
}

func (c *fakeClocks) sinceStart() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.elapsed
}

func (c *fakeClocks) after(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	if c.hold > 0 && len(c.waits) > c.hold {
		return make(chan time.Time)
	}
	c.elapsed += d
	if c.wallMoves {
		c.wall = c.wall.Add(d)
	}
	ch := make(chan time.Time, 1)
	ch <- c.wall
	return ch
}

func (c *fakeClocks) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// TestRenewerKeepsRetryingAfterExpiry starts with a credential that
// expired an hour ago, as a daemon restarted after a control-plane outage
// does: the renewer does not give up, attempts at once, retries with
// backoff bounded by ExpiredRetryCap while the control plane refuses,
// carries the expiry in the reason the heartbeat reports, and installs
// the renewed credential when an attempt succeeds.
func TestRenewerKeepsRetryingAfterExpiry(t *testing.T) {
	h, authority, id, store := newHolder(t, time.Hour)
	expiredAt := h.Leaf().NotAfter
	clocks := &fakeClocks{wall: expiredAt.Add(time.Hour), wallMoves: true, hold: 3}
	var mu sync.Mutex
	calls := 0
	var reasons []string
	renewed := make(chan *x509.Certificate, 1)
	var r *credential.Renewer
	r = &credential.Renewer{
		Holder: h, Server: "unused",
		RetryBase: time.Minute, RetryCap: time.Hour,
		Now: clocks.now, Elapsed: clocks.sinceStart, After: clocks.after,
		Renew: func(_ context.Context, key *ecdsa.PrivateKey, opts credential.RenewOptions) (*credential.RenewResult, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			reasons = append(reasons, r.LastError())
			if opts.Credential.Leaf.NotAfter != expiredAt {
				t.Errorf("renewal presented a credential other than the expired one")
			}
			if calls <= 2 {
				return nil, errors.New("control plane unreachable")
			}
			return &credential.RenewResult{WorkloadID: id, CertificatePEM: issue(t, authority, key, id, 24*time.Hour)}, nil
		},
		Renewed: func(c *x509.Certificate) { renewed <- c },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case c := <-renewed:
		if !c.NotAfter.After(expiredAt) {
			t.Fatalf("renewed credential expires %v", c.NotAfter)
		}
	case err := <-done:
		t.Fatalf("renewer returned %v with an expired credential; it must keep retrying", err)
	case <-time.After(10 * time.Second):
		t.Fatal("no renewal")
	}
	if r.LastError() != "" {
		t.Fatalf("last error after success = %q", r.LastError())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, want nil when ctx ends", err)
	}

	waits := clocks.recorded()
	if waits[0] != 0 {
		t.Fatalf("first attempt waited %v; an expired credential renews at once", waits[0])
	}
	for _, w := range waits[1:3] {
		if w < 0 || w > credential.ExpiredRetryCap {
			t.Fatalf("retry wait %v beyond the expired cap %v", w, credential.ExpiredRetryCap)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Fatalf("renew calls = %d, want 3 (two refused, one granted)", calls)
	}
	if !strings.Contains(reasons[2], "credential expired at") || !strings.Contains(reasons[2], "control plane unreachable") {
		t.Fatalf("reason while expired = %q, want the expiry and the refusal", reasons[2])
	}
	onDisk, _, err := store.Load()
	if err != nil || !onDisk.Leaf.NotAfter.After(expiredAt) {
		t.Fatalf("disk holds %v (err %v)", onDisk.Leaf, err)
	}
}

// TestRenewerSchedulesOnElapsedTime is a host whose wall clock runs three
// hours behind: by the wall clock alone the renewal point is three hours
// late, and a credential renewed at the last moment would lapse. Elapsed
// time since the credential was obtained says renew first, and wins.
func TestRenewerSchedulesOnElapsedTime(t *testing.T) {
	h, authority, id, _ := newHolder(t, 24*time.Hour)
	leaf := h.Leaf()
	lifetime := leaf.NotAfter.Sub(leaf.NotBefore)
	behind := 3 * time.Hour
	clocks := &fakeClocks{wall: leaf.NotBefore.Add(-behind), hold: 1}
	renewed := make(chan *x509.Certificate, 1)
	r := &credential.Renewer{
		Holder: h, Server: "unused",
		Now: clocks.now, Elapsed: clocks.sinceStart, After: clocks.after,
		Renew: func(_ context.Context, key *ecdsa.PrivateKey, _ credential.RenewOptions) (*credential.RenewResult, error) {
			return &credential.RenewResult{WorkloadID: id, CertificatePEM: issue(t, authority, key, id, 24*time.Hour)}, nil
		},
		Renewed: func(c *x509.Certificate) { renewed <- c },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()
	select {
	case <-renewed:
	case <-time.After(10 * time.Second):
		t.Fatal("no renewal")
	}
	first := clocks.recorded()[0]
	lo := time.Duration(float64(lifetime) * (credential.RenewFraction - credential.RenewJitterFraction))
	hi := time.Duration(float64(lifetime) * credential.RenewFraction)
	if first < lo || first >= hi {
		t.Fatalf("first wait %v not in [%v, %v): the renewal point by elapsed time", first, lo, hi)
	}
}

// TestRenewerNoticesExpiryOnElapsedTime keeps the wall clock stuck at
// issue while elapsed time runs past the lifetime and every attempt
// fails: the renewer notices the credential has expired, says so in the
// reason it reports, and keeps retrying at the expired cadence.
func TestRenewerNoticesExpiryOnElapsedTime(t *testing.T) {
	h, _, _, _ := newHolder(t, time.Hour)
	leaf := h.Leaf()
	clocks := &fakeClocks{wall: leaf.NotBefore, hold: 40}
	attempts := make(chan string, 64)
	var r *credential.Renewer
	r = &credential.Renewer{
		Holder: h, Server: "unused",
		RetryBase: time.Minute, RetryCap: time.Hour,
		Now: clocks.now, Elapsed: clocks.sinceStart, After: clocks.after,
		Renew: func(context.Context, *ecdsa.PrivateKey, credential.RenewOptions) (*credential.RenewResult, error) {
			attempts <- r.LastError()
			return nil, errors.New("refused")
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case reason := <-attempts:
			if !strings.Contains(reason, "credential expired at") {
				continue
			}
			if clocks.sinceStart() < time.Hour-ca.BackdateNotBefore {
				t.Fatalf("reported expired after %v of a one-hour credential", clocks.sinceStart())
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("Run = %v", err)
			}
			return
		case err := <-done:
			t.Fatalf("renewer returned %v; expiry is never fatal", err)
		case <-deadline:
			t.Fatal("expiry by elapsed time never noticed")
		}
	}
}

// TestRenewerStopsWhenReenrolled enrolls the state directory again under
// a running renewer, as `innerwall-agent enroll` over an expired
// credential does: the renewer stops with ErrReenrolled rather than
// renewing the old identity or writing its certificate beside the new
// key.
func TestRenewerStopsWhenReenrolled(t *testing.T) {
	h, authority, oldID, store := newHolder(t, time.Hour)
	bundle, _ := authority.Bundle(context.Background())
	renewedOld := issue(t, authority, h.Key(), oldID, time.Hour)
	newKey, _ := credential.GenerateKey()
	newID, _ := identity.NewWorkloadID()
	newCert := issue(t, authority, newKey, newID, time.Hour)
	if err := store.Save(newKey, newCert, bundle); err != nil {
		t.Fatal(err)
	}
	if err := h.Replace(renewedOld, bundle); !errors.Is(err, credential.ErrReenrolled) {
		t.Fatalf("Replace over a re-enrolled directory: err = %v", err)
	}
	clocks := &fakeClocks{wall: h.Leaf().NotAfter.Add(time.Minute)}
	r := &credential.Renewer{
		Holder: h, Server: "unused",
		Now: clocks.now, Elapsed: clocks.sinceStart, After: clocks.after,
		Renew: func(context.Context, *ecdsa.PrivateKey, credential.RenewOptions) (*credential.RenewResult, error) {
			t.Error("renewed the old identity over a re-enrolled directory")
			return nil, errors.New("unreachable")
		},
	}
	if err := r.Run(context.Background()); !errors.Is(err, credential.ErrReenrolled) {
		t.Fatalf("Run = %v, want ErrReenrolled", err)
	}
	onDisk, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := identity.FromCertificate(onDisk.Leaf); got != newID {
		t.Fatalf("disk holds %v, want the new enrollment %v", got, newID)
	}
}
