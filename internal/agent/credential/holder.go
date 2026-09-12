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
	"time"
)

// ErrCredentialExpired is returned by the renewer when the credential is
// already past its expiry: the workload can no longer authenticate, so it
// cannot renew, and only a new enrollment with a fresh provisioning token
// restores it. The daemon reports this and does not attempt enrollment
// itself (ADR-0020).
var ErrCredentialExpired = errors.New("credential: expired; re-enrollment with a new provisioning token is required")

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
// memory, so a crash between the two leaves the newer one on disk.
func (h *Holder) Replace(certPEM, bundlePEM []byte) error {
	cred, err := tls.X509KeyPair(certPEM, pemKey(h.key))
	if err != nil {
		return fmt.Errorf("credential: renewed certificate does not pair with the key: %w", err)
	}
	cred.Leaf, err = x509.ParseCertificate(cred.Certificate[0])
	if err != nil {
		return fmt.Errorf("credential: parsing renewed certificate: %w", err)
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

// RenewFraction is the point in a credential's lifetime at which the
// daemon renews: when less than one third remains.
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

// Renewer renews the holder's credential on a timer.
type Renewer struct {
	Holder *Holder
	// Server is the control-plane address for RenewCredential.
	Server string
	Log    *slog.Logger
	// RetryBase and RetryCap bound the backoff after a failed renewal;
	// one minute and one hour when zero.
	RetryBase time.Duration
	RetryCap  time.Duration
	// Now is the clock; time.Now if nil.
	Now func() time.Time
	// After is the timer; time.After if nil. Tests substitute it to run
	// the schedule without waiting.
	After func(d time.Duration) <-chan time.Time
	// Renew performs one renewal; the package's Renew when nil. Tests
	// substitute it.
	Renew func(ctx context.Context, key *ecdsa.PrivateKey, opts RenewOptions) (*RenewResult, error)
	// Renewed, when set, is called after each successful renewal.
	Renewed func(leaf *x509.Certificate)
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

// Run renews until ctx ends. It returns ErrCredentialExpired when the
// credential expires before a renewal succeeds; the caller decides what
// that means for the process. Failed attempts retry with jittered
// exponential backoff and are logged at error level every time, because
// each one brings the workload closer to needing re-enrollment.
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
	rng := rand.New(rand.NewPCG(uint64(r.now().UnixNano()), 0)) //nolint:gosec // jitter, not secrecy
	attempt := 0
	for {
		leaf := r.Holder.Leaf()
		now := r.now()
		if !now.Before(leaf.NotAfter) {
			r.log().Error("workload credential has expired; re-enrollment with a new provisioning token is required", "expired_at", leaf.NotAfter)
			return ErrCredentialExpired
		}
		var wait time.Duration
		if attempt == 0 {
			at := RenewAt(leaf.NotBefore, leaf.NotAfter, rng)
			wait = at.Sub(now)
			if wait < 0 {
				wait = 0
			}
			r.log().Info("credential renewal scheduled", "at", now.Add(wait), "expires", leaf.NotAfter)
		} else {
			wait = backoff(attempt, retryBase, retryCap, rng)
			// Never wait past expiry; a last attempt just before it is
			// worth more than a tidy schedule.
			if until := leaf.NotAfter.Sub(now); wait > until {
				wait = until / 2
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-after(wait):
		}
		cred, trust := r.Holder.Current()
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		res, err := renew(rctx, r.Holder.Key(), RenewOptions{Server: r.Server, Credential: cred, Trust: trust})
		cancel()
		if err == nil {
			err = r.Holder.Replace(res.CertificatePEM, res.BundlePEM)
		}
		if err != nil {
			attempt++
			r.log().Error("credential renewal failed; will retry", "error", err, "attempt", attempt, "expires", leaf.NotAfter)
			continue
		}
		attempt = 0
		newLeaf := r.Holder.Leaf()
		r.log().Info("credential renewed", "serial", newLeaf.SerialNumber.Text(16), "expires", newLeaf.NotAfter)
		if r.Renewed != nil {
			r.Renewed(newLeaf)
		}
	}
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
