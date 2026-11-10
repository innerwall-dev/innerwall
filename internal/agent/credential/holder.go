package credential

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// ReenrollRequired is the remedy for a credential expired beyond the
// control plane's renewal grace, in the words the control plane's refusal
// uses, so the agent's log and the refusal it carries agree.
const ReenrollRequired = "re-enrollment with a new provisioning token is required"

// ErrReenrolled is returned by the renewer when the state directory no
// longer holds this daemon's key: an operator enrolled the host again
// underneath it. The daemon cannot run as two workloads, and writing a
// renewed certificate beside the new key would pair it with the wrong
// one, so it stops and the restarted daemon runs as the new workload.
var ErrReenrolled = errors.New("credential: the state directory holds a different key (the host was enrolled again); restart the daemon to run as the new workload")

// Holder is the credential a running daemon presents, swappable in place.
// Connections opened while it holds one certificate keep using it; the
// client TLS configuration it builds asks the holder for the certificate
// at each handshake, so a renewal takes effect on the next connection
// without dropping the current stream.
type Holder struct {
	store Store
	key   *ecdsa.PrivateKey

	mu    sync.RWMutex
	cred  tls.Certificate
	trust []byte
}

// LoadHolder reads the stored credential into a holder.
func LoadHolder(store Store) (*Holder, error) {
	cred, trust, err := store.Load()
	if err != nil {
		return nil, err
	}
	key, err := store.LoadKey()
	if err != nil {
		return nil, err
	}
	return &Holder{store: store, key: key, cred: cred, trust: trust}, nil
}

// Key returns the workload private key.
func (h *Holder) Key() *ecdsa.PrivateKey { return h.key }

// Leaf returns the current certificate.
func (h *Holder) Leaf() *x509.Certificate {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cred.Leaf
}

// Current returns the current credential and trust bundle.
func (h *Holder) Current() (tls.Certificate, []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cred, h.trust
}

// Replace installs a renewed certificate, on disk first and then in
// memory, so a crash between the two leaves the newer one on disk. It
// returns ErrReenrolled, and writes nothing, when the state directory no
// longer holds this holder's key.
func (h *Holder) Replace(certPEM, bundlePEM []byte) error {
	cred, err := tls.X509KeyPair(certPEM, pemKey(h.key))
	if err != nil {
		return fmt.Errorf("credential: renewed certificate does not pair with the key: %w", err)
	}
	cred.Leaf, err = x509.ParseCertificate(cred.Certificate[0])
	if err != nil {
		return fmt.Errorf("credential: parsing renewed certificate: %w", err)
	}
	if err := h.checkStoredKey(); err != nil {
		return err
	}
	if err := h.store.ReplaceCertificate(certPEM, bundlePEM); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cred = cred
	if len(bundlePEM) > 0 {
		h.trust = bundlePEM
	}
	return nil
}

// checkStoredKey returns ErrReenrolled when the state directory's key is
// no longer this holder's. Enrollment overwrites a directory whose
// credential has expired, and the daemon keeps running across expiry, so
// the directory can change underneath it.
func (h *Holder) checkStoredKey() error {
	stored, err := h.store.LoadKey()
	if errors.Is(err, ErrNotEnrolled) {
		return ErrReenrolled
	}
	if err != nil {
		return err
	}
	if !stored.Equal(h.key) {
		return ErrReenrolled
	}
	return nil
}

// TLSConfig builds a client configuration that presents whatever
// certificate the holder has at handshake time and trusts the bundle it
// has now.
func (h *Holder) TLSConfig() (*tls.Config, error) {
	_, trust := h.Current()
	cfg, err := TLSConfig(nil, trust)
	if err != nil {
		return nil, err
	}
	cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		cred, _ := h.Current()
		return &cred, nil
	}
	return cfg, nil
}

// RenewFraction is the point in a credential's lifetime by which the
// daemon begins renewal: the two-thirds mark. Jitter moves the first
// attempt earlier, never later, so at least a third of the lifetime
// always remains for retries.
const RenewFraction = 2.0 / 3.0

// RenewJitterFraction is the width of the jitter window before the renewal
// point, as a fraction of lifetime, so that a fleet enrolled together does
// not renew together.
const RenewJitterFraction = 1.0 / 12.0

// RenewAt returns when a credential valid from notBefore to notAfter
// should be renewed: two thirds of the way through its lifetime, pulled
// earlier by a random amount up to RenewJitterFraction of the lifetime.
func RenewAt(notBefore, notAfter time.Time, rng *rand.Rand) time.Time {
	lifetime := notAfter.Sub(notBefore)
	if lifetime <= 0 {
		return notAfter
	}
	point := notBefore.Add(time.Duration(float64(lifetime) * RenewFraction))
	window := time.Duration(float64(lifetime) * RenewJitterFraction)
	if window > 0 {
		point = point.Add(-time.Duration(rng.Int64N(int64(window))))
	}
	return point
}

// ExpiredRetryCap bounds the backoff between renewal attempts once the
// credential has expired, whatever RetryCap says. Renewal is then the
// daemon's only way back to the control plane (the sync stream and flow
// reports are refused until it succeeds), so a control plane that returns
// from an outage hears from each expired agent within this long; it is the
// sync stream's reconnect cap, a load the control plane already carries.
const ExpiredRetryCap = 5 * time.Minute

// Renewer renews the holder's credential on a timer.
//
// Expiry is never fatal. Renewal begins by the two-thirds mark of a
// credential's lifetime, leaving at least the last third for retries; if
// every attempt fails until it expires (the control plane was unreachable
// through the whole window), the renewer keeps retrying with backoff,
// because the control plane accepts an expired credential for renewal,
// and nothing else, within its renewal grace (ADR-0016 as amended).
// Enforcement meanwhile continues from the persisted policy, and
// everything else resumes on the fresh credential. Beyond the grace the
// refusals say re-enrollment is required; the renewer keeps trying until
// the operator intervenes, and the reason rides every heartbeat that
// gets through.
//
// The renewal point is taken from two clocks, whichever says renew first:
// the wall clock against the certificate's validity, and the monotonic
// time elapsed since the credential was obtained. A host whose wall clock
// runs behind would otherwise sleep through its renewal window.
//
// The opposite case is a host that is suspended or paused: its monotonic
// clock, and every timer on it, stands still while the wall clock moves
// on, so a timer set for the renewal point still believes hours remain
// after the point has passed. Every wait therefore also has a wall-clock
// deadline, and Check asks the renewer to compare the wall clock with it:
// a wait whose deadline has passed ends at once, as if its timer had
// fired. The daemon calls Check at moments it has just reached the
// control plane (the sync stream's establishment and each heartbeat), so
// the check costs no request and no timer of its own. A check never moves
// the schedule: the renewal point is chosen once per credential, a check
// before a deadline shortens nothing, and the backoff count carries on.
type Renewer struct {
	Holder *Holder
	// Server is the control-plane address for RenewCredential.
	Server string
	Log    *slog.Logger
	// RetryBase and RetryCap bound the backoff after a failed renewal;
	// one minute and one hour when zero. ExpiredRetryCap bounds it
	// further once the credential has expired.
	RetryBase time.Duration
	RetryCap  time.Duration
	// Now is the wall clock; time.Now if nil.
	Now func() time.Time
	// Elapsed is a monotonic clock: the time since an arbitrary fixed
	// origin, unaffected by changes to the wall clock. Measured from the
	// renewer's start when nil. Tests substitute it.
	Elapsed func() time.Duration
	// After is the timer; time.After if nil. Tests substitute it to run
	// the schedule without waiting.
	After func(d time.Duration) <-chan time.Time
	// Renew performs one renewal; the package's Renew when nil. Tests
	// substitute it.
	Renew func(ctx context.Context, key *ecdsa.PrivateKey, opts RenewOptions) (*RenewResult, error)
	// Renewed, when set, is called after each successful renewal.
	Renewed func(leaf *x509.Certificate)

	lastError atomic.Value // string

	wakeOnce sync.Once
	wake     chan struct{} // capacity one; see Check
}

// Check asks the renewer to compare the wall clock with the deadline of
// the wait it is in, and to end the wait now if the deadline has passed.
// It never blocks and never queues more than one check: a check made
// while another is pending is the same check. It is safe to call at any
// time, before Run starts, while it runs, and after it returns.
func (r *Renewer) Check() {
	select {
	case r.wakeChan() <- struct{}{}:
	default:
	}
}

func (r *Renewer) wakeChan() chan struct{} {
	r.wakeOnce.Do(func() { r.wake = make(chan struct{}, 1) })
	return r.wake
}

// LastError returns the reason the most recent renewal attempt failed, or
// empty when the last attempt succeeded or none has been due. Once the
// credential has expired the reason says so, with the expiry. The daemon
// carries it on every heartbeat so the control plane sees a workload
// heading for expiry before it gets there.
func (r *Renewer) LastError() string {
	v, _ := r.lastError.Load().(string)
	return v
}

func (r *Renewer) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Renewer) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// lifeClock tracks one credential's age on the monotonic clock: the age
// it had when the daemon obtained it, by the wall clock and never less
// than zero, plus the monotonic time since. A certificate loaded from
// disk at start is aged from its notBefore by the wall clock, the only
// evidence there is; a renewed one is aged from its receipt, which is
// within a few minutes of its issue.
type lifeClock struct {
	obtained time.Duration // Elapsed() at receipt
	ageThen  time.Duration // age at receipt
}

func (c lifeClock) age(elapsed time.Duration) time.Duration {
	return c.ageThen + (elapsed - c.obtained)
}

func newLifeClock(leaf *x509.Certificate, now time.Time, elapsed time.Duration) lifeClock {
	return lifeClock{obtained: elapsed, ageThen: max(0, now.Sub(leaf.NotBefore))}
}

// Run renews until ctx ends, and returns nil then. It returns early only
// with ErrReenrolled, when the host was enrolled again underneath it.
// Failed attempts retry with jittered exponential backoff and are logged
// at error level every time, because each one brings the workload closer
// to expiry, or further past it.
func (r *Renewer) Run(ctx context.Context) error {
	retryBase, retryCap := r.RetryBase, r.RetryCap
	if retryBase <= 0 {
		retryBase = time.Minute
	}
	if retryCap <= 0 {
		retryCap = time.Hour
	}
	renew := r.Renew
	if renew == nil {
		renew = Renew
	}
	after := r.After
	if after == nil {
		after = time.After
	}
	elapsed := r.Elapsed
	if elapsed == nil {
		start := time.Now()
		elapsed = func() time.Duration { return time.Since(start) }
	}
	wake := r.wakeChan()
	rng := rand.New(rand.NewPCG(uint64(r.now().UnixNano()), 0)) //nolint:gosec // jitter, not secrecy
	attempt := 0
	first := r.Holder.Leaf()
	clock := newLifeClock(first, r.now(), elapsed())
	// The renewal point is chosen once per credential, so nothing that
	// wakes the loop can re-roll its jitter.
	renewAt := RenewAt(first.NotBefore, first.NotAfter, rng)
	reportedExpiry := false
	for {
		leaf := r.Holder.Leaf()
		now := r.now()
		lifetime := leaf.NotAfter.Sub(leaf.NotBefore)
		age := clock.age(elapsed())
		expired := !now.Before(leaf.NotAfter) || age >= lifetime
		// wait is the timer; deadline is when the same wait ends by the
		// wall clock, which a check compares against (see await).
		var wait time.Duration
		var deadline time.Time
		switch {
		case expired:
			if !reportedExpiry {
				reportedExpiry = true
				r.log().Error("workload credential has expired; enforcement continues from the persisted policy and renewal keeps retrying, which the control plane accepts within its renewal grace; beyond the grace, "+ReenrollRequired,
					"expired_at", leaf.NotAfter, "wall_clock", now)
			}
			if attempt > 0 {
				wait = backoff(attempt, retryBase, min(retryCap, ExpiredRetryCap), rng)
			}
			deadline = now.Add(wait)
		case attempt == 0:
			byWall := renewAt.Sub(now)
			byMonotonic := renewAt.Sub(leaf.NotBefore) - age
			clockName := "wall"
			wait = byWall
			if byMonotonic < byWall {
				wait, clockName = byMonotonic, "monotonic"
			}
			wait = max(wait, 0)
			// By the wall clock the wait ends at the renewal point itself,
			// or now if that has passed.
			deadline = renewAt
			if deadline.Before(now) {
				deadline = now
			}
			r.log().Info("credential renewal scheduled", "at", now.Add(wait), "expires", leaf.NotAfter, "by_clock", clockName)
			if clockName == "monotonic" && byWall-byMonotonic > time.Minute {
				r.log().Warn("the wall clock is behind the credential's age; renewing on elapsed time instead", "wall_says", byWall.Round(time.Second), "elapsed_says", byMonotonic.Round(time.Second))
			}
		default:
			wait = backoff(attempt, retryBase, retryCap, rng)
			// Never wait past expiry: from there the expired cadence
			// applies, which retries sooner.
			wait = min(wait, leaf.NotAfter.Sub(now), lifetime-age)
			deadline = now.Add(wait)
		}
		if !r.await(ctx, wait, deadline, leaf.NotAfter, after, elapsed, wake) {
			return nil
		}
		if err := r.Holder.checkStoredKey(); errors.Is(err, ErrReenrolled) {
			return r.reenrolled(err)
		}
		cred, trust := r.Holder.Current()
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		res, err := renew(rctx, r.Holder.Key(), RenewOptions{Server: r.Server, Credential: cred, Trust: trust})
		cancel()
		if err == nil {
			err = r.Holder.Replace(res.CertificatePEM, res.BundlePEM)
		}
		if errors.Is(err, ErrReenrolled) {
			return r.reenrolled(err)
		}
		if err != nil {
			attempt++
			reason := err.Error()
			if expired {
				reason = fmt.Sprintf("credential expired at %s; renewal failed: %v", leaf.NotAfter.UTC().Format(time.RFC3339), err)
			}
			r.lastError.Store(reason)
			r.log().Error("credential renewal failed; will retry", "error", err, "attempt", attempt, "expires", leaf.NotAfter, "expired", expired)
			continue
		}
		attempt = 0
		reportedExpiry = false
		r.lastError.Store("")
		newLeaf := r.Holder.Leaf()
		clock = newLifeClock(newLeaf, r.now(), elapsed())
		renewAt = RenewAt(newLeaf.NotBefore, newLeaf.NotAfter, rng)
		if expired {
			r.log().Warn("credential renewed after it had expired", "serial", newLeaf.SerialNumber.Text(16), "expired_at", leaf.NotAfter, "expires", newLeaf.NotAfter)
		} else {
			r.log().Info("credential renewed", "serial", newLeaf.SerialNumber.Text(16), "expires", newLeaf.NotAfter)
		}
		if r.Renewed != nil {
			r.Renewed(newLeaf)
		}
	}
}

// await waits out one scheduled wait of length wait, whose wall-clock
// deadline is deadline, and reports whether it ended (false when ctx did).
// It ends when the wait's timer fires, or when a check finds the wall
// clock at or past the deadline. A check before the deadline ends
// nothing; if the wall clock has gained on the timer, which happens when
// the host is suspended or paused mid-wait, it arms one more timer for the
// wall clock's remainder, so the wait ends at whichever comes first and is
// never extended. A check that finds the clocks in step arms nothing.
func (r *Renewer) await(ctx context.Context, wait time.Duration, deadline, expires time.Time,
	after func(time.Duration) <-chan time.Time, elapsed func() time.Duration, wake <-chan struct{},
) bool {
	deadline = deadline.Round(0) // compared and logged as wall-clock time only
	firesBy := elapsed() + wait  // when, on the monotonic clock, a timer here fires
	timer := after(wait)
	var wallTimer <-chan time.Time // nil, so never ready, until armed
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer:
			return true
		case <-wallTimer:
			return true
		case <-wake:
		}
		// The wall clock alone: time.Now's monotonic reading, which
		// comparisons otherwise prefer, stands still with the timer.
		now := r.now().Round(0)
		if !now.Before(deadline) {
			if now.Sub(deadline) > time.Minute {
				r.log().Warn("the renewal point passed while the timer was not running (the host was suspended or paused); renewing now", "renew_at", deadline, "wall_clock", now, "expires", expires)
			}
			return true
		}
		e := elapsed()
		if remaining := deadline.Sub(now); e+remaining < firesBy-time.Second {
			firesBy = e + remaining
			wallTimer = after(remaining)
		}
	}
}

func (r *Renewer) reenrolled(err error) error {
	r.log().Error("the state directory was enrolled again underneath this daemon; stopping so the restarted daemon runs as the new workload")
	return err
}

// backoff is exponential with full jitter (ADR-0002).
func backoff(attempt int, base, limit time.Duration, rng *rand.Rand) time.Duration {
	ceiling := base << uint(min(attempt, 20)) //nolint:gosec // bounded
	if ceiling > limit || ceiling <= 0 {
		ceiling = limit
	}
	return time.Duration(rng.Int64N(int64(ceiling)))
}

func pemKey(key *ecdsa.PrivateKey) []byte {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil
	}
	return encodePEM("PRIVATE KEY", der)
}
