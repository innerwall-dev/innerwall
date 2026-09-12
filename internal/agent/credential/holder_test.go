package credential_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"math/rand/v2"
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

func TestRenewerReportsExpiry(t *testing.T) {
	h, _, _, _ := newHolder(t, time.Hour)
	leaf := h.Leaf()
	r := &credential.Renewer{
		Holder: h, Server: "unused",
		Now: func() time.Time { return leaf.NotAfter.Add(time.Second) },
		Renew: func(context.Context, *ecdsa.PrivateKey, credential.RenewOptions) (*credential.RenewResult, error) {
			t.Fatal("renewal attempted with an expired credential")
			return nil, nil
		},
	}
	if err := r.Run(context.Background()); !errors.Is(err, credential.ErrCredentialExpired) {
		t.Fatalf("err = %v, want ErrCredentialExpired", err)
	}
}
