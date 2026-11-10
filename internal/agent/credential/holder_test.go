package credential_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"log/slog"
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

// pausableClocks drives a renewer across a host suspend: the wall clock
// and the monotonic clock move only when the test moves them, each on its
// own, and a timer fires only when the test fires it.
type pausableClocks struct {
	mu      sync.Mutex
	wall    time.Time
	elapsed time.Duration
	timers  []chan time.Time
	watch   bool
	// scheduled receives every wait the renewer sets a timer for; reads
	// receives one signal per wall-clock read while watch is on.
	scheduled chan time.Duration
	reads     chan struct{}
}

func newPausableClocks(wall time.Time) *pausableClocks {
	return &pausableClocks{wall: wall, scheduled: make(chan time.Duration, 64), reads: make(chan struct{}, 256)}
}

func (c *pausableClocks) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watch {
		c.reads <- struct{}{}
	}
	return c.wall
}

func (c *pausableClocks) sinceStart() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.elapsed
}

func (c *pausableClocks) after(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, ch)
	c.scheduled <- d
	return ch
}

// run moves both clocks on by d, as a host that is running does.
func (c *pausableClocks) run(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(d)
	c.elapsed += d
}

// stepWall moves the wall clock alone: forward by a suspend, during which
// the monotonic clock and every timer stand still, or backward by a step.
func (c *pausableClocks) stepWall(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(d)
}

// fire fires the i-th timer the renewer set.
func (c *pausableClocks) fire(i int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timers[i] <- c.wall
}

func (c *pausableClocks) timerCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func (c *pausableClocks) watching(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.watch = on
}

// lockedBuffer collects a renewer's log lines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) count(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), s)
}

const suspendedLine = "the renewal point passed while the timer was not running"

// suspendRig runs a renewer on pausable clocks over a 24-hour credential,
// an hour into its life. Renewal attempts fail while fail says so and
// otherwise return a 72-hour credential, long enough that its own renewal
// point lies beyond anything the tests do to the clocks.
type suspendRig struct {
	t        *testing.T
	leaf     *x509.Certificate
	clocks   *pausableClocks
	r        *credential.Renewer
	logs     *lockedBuffer
	attempts chan struct{}
	cancel   context.CancelFunc
	done     chan error

	mu    sync.Mutex
	calls int
}

func startSuspendRig(t *testing.T, retryBase time.Duration, fail func(call int) bool) *suspendRig {
	t.Helper()
	h, authority, id, _ := newHolder(t, 24*time.Hour)
	g := &suspendRig{
		t:        t,
		leaf:     h.Leaf(),
		clocks:   newPausableClocks(h.Leaf().NotBefore.Add(time.Hour)),
		logs:     &lockedBuffer{},
		attempts: make(chan struct{}, 64),
		done:     make(chan error, 1),
	}
	g.r = &credential.Renewer{
		Holder: h, Server: "unused",
		Log:       slog.New(slog.NewTextHandler(g.logs, nil)),
		RetryBase: retryBase, RetryCap: time.Hour,
		Now: g.clocks.now, Elapsed: g.clocks.sinceStart, After: g.clocks.after,
		Renew: func(_ context.Context, key *ecdsa.PrivateKey, _ credential.RenewOptions) (*credential.RenewResult, error) {
			g.mu.Lock()
			g.calls++
			n := g.calls
			g.mu.Unlock()
			g.attempts <- struct{}{}
			if fail(n) {
				return nil, errors.New("control plane unreachable")
			}
			return &credential.RenewResult{WorkloadID: id, CertificatePEM: issue(t, authority, key, id, 72*time.Hour)}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	go func() { g.done <- g.r.Run(ctx) }()
	t.Cleanup(cancel)
	return g
}

// scheduled returns the next wait the renewer sets a timer for.
func (g *suspendRig) scheduled() time.Duration {
	g.t.Helper()
	select {
	case d := <-g.clocks.scheduled:
		return d
	case <-time.After(10 * time.Second):
		g.t.Fatal("no wait scheduled")
		return 0
	}
}

// attempt waits for the next renewal attempt.
func (g *suspendRig) attempt() {
	g.t.Helper()
	select {
	case <-g.attempts:
	case <-time.After(10 * time.Second):
		g.t.Fatal("no renewal attempt")
	}
}

// settle checks twice and returns once the renewer has taken the second
// check, so the first has been handled to the end. A check is a single
// pending wake, so the second is taken only after the first is done.
func (g *suspendRig) settle() {
	g.t.Helper()
	g.clocks.watching(true)
	defer g.clocks.watching(false)
	for range 2 {
		g.r.Check()
		select {
		case <-g.clocks.reads:
		case <-time.After(10 * time.Second):
			g.t.Fatal("the renewer did not take the check")
		}
	}
}

func (g *suspendRig) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func (g *suspendRig) stop() {
	g.t.Helper()
	g.cancel()
	if err := <-g.done; err != nil {
		g.t.Fatalf("Run = %v, want nil when ctx ends", err)
	}
}

func never(int) bool  { return false }
func always(int) bool { return true }

// TestRenewerActsOnRenewalPointPassedDuringSuspend is a host suspended
// across its renewal point: the wall clock moves past it while elapsed
// time and the timer stand still. A check renews at once, exactly once,
// and says why in one warning.
func TestRenewerActsOnRenewalPointPassedDuringSuspend(t *testing.T) {
	g := startSuspendRig(t, time.Minute, never)
	g.scheduled()
	g.clocks.stepWall(19 * time.Hour) // twenty hours into a 24-hour credential
	g.r.Check()
	g.attempt()
	g.scheduled() // the renewed credential's schedule: the loop is waiting again
	g.settle()
	if n := g.callCount(); n != 1 {
		t.Fatalf("renew calls = %d, want exactly 1", n)
	}
	if n := g.logs.count(suspendedLine); n != 1 {
		t.Fatalf("suspend warning logged %d times, want once", n)
	}
	for line := range strings.Lines(g.logs.String()) {
		if strings.Contains(line, suspendedLine) && (!strings.Contains(line, "level=WARN") || !strings.Contains(line, "renew_at=") || !strings.Contains(line, "wall_clock=") || !strings.Contains(line, "expires=")) {
			t.Fatalf("the warning does not name the renewal point, the wall clock, and the expiry: %s", line)
		}
	}
	if g.clocks.timerCount() != 2 {
		t.Fatalf("%d timers set, want 2 (the first credential's and the renewed one's)", g.clocks.timerCount())
	}
	g.stop()
	// Checks after Run has returned neither block nor panic.
	g.r.Check()
	g.r.Check()
}

// TestRenewerCheckBeforeRenewalPointChangesNothing: a check before the
// renewal point renews nothing, sets no timer, and leaves the renewal
// point where it was scheduled, to the nanosecond.
func TestRenewerCheckBeforeRenewalPointChangesNothing(t *testing.T) {
	g := startSuspendRig(t, time.Minute, never)
	d := g.scheduled()
	g.clocks.run(d - time.Second)
	g.settle()
	if n := g.callCount(); n != 0 {
		t.Fatalf("renew calls = %d before the renewal point", n)
	}
	if g.clocks.timerCount() != 1 {
		t.Fatalf("%d timers set; a check with the clocks in step sets none", g.clocks.timerCount())
	}
	if n := g.logs.count("credential renewal scheduled"); n != 1 {
		t.Fatalf("renewal scheduled %d times; a check must not reschedule", n)
	}
	// At the scheduled instant exactly, a check renews.
	g.clocks.run(time.Second)
	g.r.Check()
	g.attempt()
	g.scheduled()
	if n := g.logs.count(suspendedLine); n != 0 {
		t.Fatalf("suspend warning logged %d times for a renewal on time", n)
	}
	g.stop()
}

// TestRenewerManyChecksRenewOnce: checks before the renewal point cost
// nothing, and the renewal happens once, when its timer fires.
func TestRenewerManyChecksRenewOnce(t *testing.T) {
	g := startSuspendRig(t, time.Minute, never)
	d := g.scheduled()
	for range 50 {
		g.r.Check()
	}
	g.settle()
	if n := g.callCount(); n != 0 {
		t.Fatalf("renew calls = %d before the renewal point", n)
	}
	g.clocks.run(d)
	g.clocks.fire(0)
	g.attempt()
	g.scheduled()
	g.settle()
	if n := g.callCount(); n != 1 {
		t.Fatalf("renew calls = %d, want exactly 1", n)
	}
	if g.clocks.timerCount() != 2 {
		t.Fatalf("%d timers set, want 2; checks set none", g.clocks.timerCount())
	}
	g.stop()
}

// TestRenewerSuspendDuringBackoff: a failed attempt's backoff wait passes
// on the wall clock while the host is suspended; a check retries once,
// and the attempt count carries on from where it was.
func TestRenewerSuspendDuringBackoff(t *testing.T) {
	g := startSuspendRig(t, time.Minute, always)
	d := g.scheduled()
	g.clocks.run(d)
	g.clocks.fire(0)
	g.attempt()
	backoff := g.scheduled()
	g.clocks.stepWall(backoff + 10*time.Minute)
	g.r.Check()
	g.attempt()
	next := g.scheduled()
	if n := g.callCount(); n != 2 {
		t.Fatalf("renew calls = %d, want 2 (the timer's, then the check's)", n)
	}
	if g.logs.count("attempt=2") != 1 {
		t.Fatalf("the check's attempt was not the second:\n%s", g.logs)
	}
	if next < 0 || next > 4*time.Minute {
		t.Fatalf("backoff after the second failure %v, want within base << 2", next)
	}
	if n := g.logs.count(suspendedLine); n != 1 {
		t.Fatalf("suspend warning logged %d times, want once", n)
	}
	g.stop()
}

// TestRenewerSuspendPastExpiry: the host sleeps through the renewal point
// and the expiry. A check attempts at once; from there the expired path
// takes over unchanged, its cadence within ExpiredRetryCap although the
// configured backoff is longer, and the reason saying the credential
// expired.
func TestRenewerSuspendPastExpiry(t *testing.T) {
	g := startSuspendRig(t, 10*time.Minute, always)
	g.scheduled()
	g.clocks.stepWall(g.leaf.NotAfter.Sub(g.clocks.now()) + time.Hour)
	g.r.Check()
	g.attempt()
	first := g.scheduled()
	g.clocks.fire(1)
	g.attempt()
	second := g.scheduled()
	for _, w := range []time.Duration{first, second} {
		if w < 0 || w > credential.ExpiredRetryCap {
			t.Fatalf("retry wait %v beyond the expired cap %v", w, credential.ExpiredRetryCap)
		}
	}
	if n := g.logs.count("workload credential has expired"); n != 1 {
		t.Fatalf("expiry logged %d times, want once", n)
	}
	if reason := g.r.LastError(); !strings.Contains(reason, "credential expired at") {
		t.Fatalf("reason while expired = %q", reason)
	}
	if n := g.logs.count(suspendedLine); n != 1 {
		t.Fatalf("suspend warning logged %d times, want once", n)
	}
	g.stop()
}

// TestRenewerWallClockStepsBack: a wall clock stepped backwards mid-wait
// says the renewal point is further off than the timer does. A check sets
// no timer and renews nothing, and the timer still ends the wait.
func TestRenewerWallClockStepsBack(t *testing.T) {
	g := startSuspendRig(t, time.Minute, never)
	d := g.scheduled()
	g.clocks.stepWall(-2 * time.Hour)
	g.settle()
	if n := g.callCount(); n != 0 {
		t.Fatalf("renew calls = %d after a backwards step", n)
	}
	if g.clocks.timerCount() != 1 {
		t.Fatalf("%d timers set; a check must not replace or extend the wait", g.clocks.timerCount())
	}
	g.clocks.run(d)
	g.clocks.fire(0)
	g.attempt()
	g.scheduled()
	if n := g.logs.count(suspendedLine); n != 0 {
		t.Fatalf("suspend warning logged %d times for a backwards step", n)
	}
	g.stop()
}

// TestRenewerWallGainsArmsRemainder: the wall clock gains on the timer
// mid-wait without yet reaching the renewal point. A check sets one timer
// for the wall clock's remainder, and whichever fires first ends the wait.
func TestRenewerWallGainsArmsRemainder(t *testing.T) {
	g := startSuspendRig(t, time.Minute, never)
	d := g.scheduled()
	g.clocks.stepWall(d - time.Hour) // suspended for all but the last hour
	g.settle()
	if g.clocks.timerCount() != 2 {
		t.Fatalf("%d timers set, want the original and one for the wall clock's remainder", g.clocks.timerCount())
	}
	if remainder := g.scheduled(); remainder != time.Hour {
		t.Fatalf("remainder timer %v, want 1h", remainder)
	}
	if n := g.callCount(); n != 0 {
		t.Fatalf("renew calls = %d before the renewal point", n)
	}
	g.clocks.run(time.Hour)
	g.clocks.fire(1)
	g.attempt()
	g.scheduled()
	if n := g.callCount(); n != 1 {
		t.Fatalf("renew calls = %d, want exactly 1", n)
	}
	g.stop()
}

// TestRenewerCheckOnZeroValue: Check is safe on a renewer that has never
// run, and never blocks however often it is called.
func TestRenewerCheckOnZeroValue(t *testing.T) {
	r := &credential.Renewer{}
	for range 3 {
		r.Check()
	}
}
