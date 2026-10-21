package enroll

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// DefaultLeafTTL is the lifetime of an issued workload credential when the
// operator sets none. Short lifetimes replace revocation machinery: a
// credential that is not renewed simply stops working (ADR-0016).
const DefaultLeafTTL = 24 * time.Hour

// DefaultRenewalGrace is how long after its expiry a workload credential
// may still renew when the operator sets no grace. It is sized to outlast
// a control-plane outage of a long weekend: a fleet whose credentials
// lapsed while nothing could renew them comes back by itself, with the
// same identities, once the control plane returns (ADR-0016 as amended).
const DefaultRenewalGrace = 72 * time.Hour

// ErrCredentialExpired is wrapped by every refusal of an expired workload
// credential: on any method other than renewal, and on renewal once the
// grace is spent. The message beyond the grace names re-enrollment, in
// the words the agent uses for the same state.
var ErrCredentialExpired = errors.New("workload credential has expired")

// ErrCredentialNotYetValid is returned for a credential whose validity
// has not begun on the control plane's clock.
var ErrCredentialNotYetValid = errors.New("workload credential is not yet valid")

// ReenrollRequired is the remedy a credential expired beyond the grace is
// told, and the phrase the agent logs for the same state.
const ReenrollRequired = "re-enrollment with a new provisioning token is required"

// ErrWorkloadUnknown is returned when a credential names a workload the
// registry does not hold.
var ErrWorkloadUnknown = errors.New("enroll: workload is not registered")

// Workload is a registry row: the identity granted at enrollment plus the
// attributes that live beside it rather than in the credential.
type Workload struct {
	ID                  identity.WorkloadID
	TokenID             uuid.UUID
	Hostname            string
	Labels              []Label
	EnrolledAt          time.Time
	CredentialSerial    string
	CredentialExpiresAt time.Time
	LastRenewedAt       *time.Time
	// LateRenewal is the latest renewal made with an already-expired
	// credential, within the grace; nil when there has been none.
	LateRenewal *LateRenewal
	// RetiredAt is when the operator retired the workload; nil while it
	// is active. A retired identity renews nothing (ADR-0023).
	RetiredAt *time.Time
}

// LateRenewal records a renewal made with a credential that had already
// expired: when it happened, and when the credential it replaced had
// expired. The difference is how late it was.
type LateRenewal struct {
	RenewedAt time.Time
	ExpiredAt time.Time
}

// Store is the persistence the enrollment domain needs. Every method is
// implemented with hand-written SQL in internal/store (ADR-0006).
type Store interface {
	// CreateToken persists a minted token (hash only).
	CreateToken(ctx context.Context, t Token) error
	// FindTokenByHash returns ErrTokenUnknown when no token has that hash.
	FindTokenByHash(ctx context.Context, hash []byte) (*Token, error)
	// ListTokens returns every token, newest first.
	ListTokens(ctx context.Context) ([]Token, error)
	// RevokeToken marks a token revoked at instant now. It returns
	// ErrTokenUnknown for an unknown id and ErrTokenRevoked if it already was.
	RevokeToken(ctx context.Context, id uuid.UUID, now time.Time) error

	// CreateWorkload persists a newly enrolled workload with its labels and
	// records the use against its token, atomically.
	CreateWorkload(ctx context.Context, w Workload, now time.Time) error
	// GetWorkload returns ErrWorkloadUnknown when the id is not registered.
	GetWorkload(ctx context.Context, id identity.WorkloadID) (*Workload, error)
	// RecordRenewal stores the serial and expiry of a renewed credential,
	// and, when late is non-nil, records it as the workload's latest late
	// renewal in the same write. It returns ErrWorkloadUnknown when the id
	// is not registered and a *registry.RetiredError when the workload was
	// retired, however recently: the write is conditioned on it.
	RecordRenewal(ctx context.Context, id identity.WorkloadID, serial string, expiresAt, now time.Time, late *LateRenewal) error
}

// Service performs enrollment and renewal.
type Service struct {
	Store     Store
	Authority ca.Authority
	// LeafTTL is the lifetime of issued credentials; DefaultLeafTTL if zero.
	LeafTTL time.Duration
	// RenewalGrace is how long after its expiry a credential may still
	// renew, and do nothing else; DefaultRenewalGrace if zero.
	RenewalGrace time.Duration
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

// Credential is what an enrollment or renewal hands back.
type Credential struct {
	CertificatePEM []byte
	BundlePEM      []byte
	Serial         string
	ExpiresAt      time.Time
	// LateBy is how long the credential a renewal replaced had been
	// expired; zero for an enrollment and for a renewal on time.
	LateBy time.Duration
}

// Result is the outcome of a successful enrollment.
type Result struct {
	Workload   Workload
	Credential Credential
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) leafTTL() time.Duration {
	if s.LeafTTL > 0 {
		return s.LeafTTL
	}
	return DefaultLeafTTL
}

// Grace returns the renewal grace in force.
func (s *Service) Grace() time.Duration {
	if s.RenewalGrace > 0 {
		return s.RenewalGrace
	}
	return DefaultRenewalGrace
}

// Standing is where a credential stands against its expiry and the
// renewal grace: what the control plane will still accept from it.
type Standing int

// Credential standings.
const (
	// StandingValid: not yet expired; it may make any call.
	StandingValid Standing = iota
	// StandingInGrace: expired within the grace; it may renew, and
	// renewing recovers the workload with the same identity.
	StandingInGrace
	// StandingPastGrace: expired beyond the grace; nothing accepts it,
	// and the workload returns only by enrolling again with a new
	// provisioning token.
	StandingPastGrace
)

// StandingAt is the standing, at now, of a credential that expires at
// notAfter under the given renewal grace. It is the rule AdmitCredential
// applies to a presented credential, and the one the operator surface
// reports a workload's stored expiry by, so the two cannot disagree.
func StandingAt(notAfter, now time.Time, grace time.Duration) Standing {
	if !now.After(notAfter) {
		return StandingValid
	}
	if now.Sub(notAfter).Truncate(time.Second) > grace {
		return StandingPastGrace
	}
	return StandingInGrace
}

// AdmitCredential decides whether a verified workload credential valid
// from notBefore to notAfter may make a call now. A credential within
// its validity may make any call. An expired one may do exactly one
// thing, renew, and only within the grace: it still proves possession of
// a key the authority bound to the identity, and refusing it would turn
// a control-plane outage longer than the renewal window into per-host
// re-enrollment, a new identity, and an orphaned history. Everything
// else (the sync stream, flow reports) keeps requiring a valid
// credential, because what those carry is acted on as coming from the
// identity now. Beyond the grace the refusal names re-enrollment.
func (s *Service) AdmitCredential(notBefore, notAfter time.Time, renewal bool) error {
	now := s.now()
	if now.Before(notBefore) {
		return fmt.Errorf("%w: its validity begins at %s and the control plane's clock reads %s", ErrCredentialNotYetValid, notBefore.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	grace := s.Grace()
	standing := StandingAt(notAfter, now, grace)
	if standing == StandingValid {
		return nil
	}
	late := now.Sub(notAfter).Truncate(time.Second)
	expired := notAfter.UTC().Format(time.RFC3339)
	if standing == StandingPastGrace {
		return fmt.Errorf("%w: it expired at %s, %s ago, beyond the %s renewal grace; %s", ErrCredentialExpired, expired, late, grace, ReenrollRequired)
	}
	if renewal {
		return nil
	}
	return fmt.Errorf("%w: it expired at %s, %s ago; an expired credential may only renew, which it may for %s more", ErrCredentialExpired, expired, late, (grace - late).Truncate(time.Second))
}

// ListTokens returns every provisioning token, newest first: hashes and
// metadata, never a plaintext.
func (s *Service) ListTokens(ctx context.Context) ([]Token, error) {
	return s.Store.ListTokens(ctx)
}

// RevokeToken revokes a token as of now. Future enrollments with it are
// refused; workloads it already enrolled are untouched (ADR-0016).
func (s *Service) RevokeToken(ctx context.Context, id uuid.UUID) error {
	return s.Store.RevokeToken(ctx, id, s.now())
}

// MintToken creates a token scoped to labels with the given lifetime
// (DefaultTokenTTL if zero) and returns the plaintext exactly once.
func (s *Service) MintToken(ctx context.Context, name string, labels []Label, ttl time.Duration) (plaintext string, tok Token, err error) {
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}
	if err := validateLabels(labels); err != nil {
		return "", Token{}, err
	}
	plaintext, hash, err := NewToken()
	if err != nil {
		return "", Token{}, err
	}
	now := s.now()
	hint := ListingHint(plaintext)
	tok = Token{
		ID:        uuid.New(),
		Hash:      hash,
		Prefix:    &hint,
		Name:      name,
		Labels:    labels,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := s.Store.CreateToken(ctx, tok); err != nil {
		return "", Token{}, err
	}
	return plaintext, tok, nil
}

// Enroll exchanges a provisioning token and a CSR for an identity and a
// credential. The order is deliberate: the token is checked before the CSR
// is even parsed, so an unauthenticated caller learns nothing about what the
// signing boundary accepts.
func (s *Service) Enroll(ctx context.Context, tokenPlaintext string, csrPEM []byte, hostname string) (*Result, error) {
	hash, err := HashToken(tokenPlaintext)
	if err != nil {
		return nil, err
	}
	tok, err := s.Store.FindTokenByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := tok.Check(now); err != nil {
		return nil, err
	}
	if err := validateLabels(tok.Labels); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTokenLabelsInvalid, err)
	}

	id, err := identity.NewWorkloadID()
	if err != nil {
		return nil, err
	}
	cred, err := s.issue(ctx, id, csrPEM)
	if err != nil {
		return nil, err
	}
	w := Workload{
		ID:                  id,
		TokenID:             tok.ID,
		Hostname:            hostname,
		Labels:              append([]Label(nil), tok.Labels...),
		EnrolledAt:          now,
		CredentialSerial:    cred.Serial,
		CredentialExpiresAt: cred.ExpiresAt,
	}
	if err := s.Store.CreateWorkload(ctx, w, now); err != nil {
		return nil, err
	}
	return &Result{Workload: w, Credential: *cred}, nil
}

// Renew reissues the credential for id, which the caller has already
// derived from a verified connection credential valid from presentedFrom
// to presentedUntil. The same identity is bound; the CSR contributes only
// its (possibly new) public key. A presented credential that has expired
// renews within the grace and is recorded on the workload as a late
// renewal; beyond it, for a workload no longer registered, or for one the
// operator retired (a *registry.RetiredError), nothing is issued. The
// retirement is checked before anything is signed, and again by the
// conditioned write, which keeps a credential signed meanwhile from being
// recorded or returned.
func (s *Service) Renew(ctx context.Context, id identity.WorkloadID, presentedFrom, presentedUntil time.Time, csrPEM []byte) (*Credential, error) {
	if err := s.AdmitCredential(presentedFrom, presentedUntil, true); err != nil {
		return nil, err
	}
	w, err := s.Store.GetWorkload(ctx, id)
	if err != nil {
		return nil, err
	}
	if w.RetiredAt != nil {
		return nil, &registry.RetiredError{ID: id, RetiredAt: *w.RetiredAt}
	}
	cred, err := s.issue(ctx, id, csrPEM)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var late *LateRenewal
	if now.After(presentedUntil) {
		late = &LateRenewal{RenewedAt: now, ExpiredAt: presentedUntil}
		cred.LateBy = now.Sub(presentedUntil)
	}
	if err := s.Store.RecordRenewal(ctx, id, cred.Serial, cred.ExpiresAt, now, late); err != nil {
		return nil, err
	}
	return cred, nil
}

// CheckNotRetired refuses an identity whose workload the operator has
// retired, with a *registry.RetiredError naming the instant. It is the
// rule every agent-service call is admitted by beside the credential's
// validity (ADR-0023); a workload the registry does not hold passes here
// and is refused by whatever needs its record, as before.
func (s *Service) CheckNotRetired(ctx context.Context, id identity.WorkloadID) error {
	w, err := s.Store.GetWorkload(ctx, id)
	if errors.Is(err, ErrWorkloadUnknown) {
		return nil
	}
	if err != nil {
		return err
	}
	if w.RetiredAt != nil {
		return &registry.RetiredError{ID: id, RetiredAt: *w.RetiredAt}
	}
	return nil
}

func (s *Service) issue(ctx context.Context, id identity.WorkloadID, csrPEM []byte) (*Credential, error) {
	certPEM, err := s.Authority.Sign(ctx, csrPEM, id, s.leafTTL())
	if err != nil {
		return nil, err
	}
	bundle, err := s.Authority.Bundle(ctx)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("enroll: authority returned no PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("enroll: authority returned an unparseable certificate: %w", err)
	}
	// The store and the wire both trust the authority; this check is the
	// domain refusing to persist a credential that does not say what it
	// was asked to say, whatever the backend.
	got, err := identity.FromCertificate(cert)
	if err != nil || got != id {
		return nil, fmt.Errorf("enroll: authority issued a certificate for %v, wanted %v", got, id)
	}
	return &Credential{
		CertificatePEM: certPEM,
		BundlePEM:      bundle,
		Serial:         CredentialSerial(cert),
		ExpiresAt:      cert.NotAfter,
	}, nil
}

// CredentialSerial is a credential's serial as the control plane records
// and names it: the certificate's serial number in lowercase hex. What
// issuance stores and what the sync path records a stream presenting are
// both written by it, so the two compare as text.
func CredentialSerial(cert *x509.Certificate) string {
	return cert.SerialNumber.Text(16)
}

// validateLabels admits a token's labels against the label grammar
// (ADR-0022), the one every label entering the system is held to; a
// refusal is the grammar's findings.
func validateLabels(labels []Label) error {
	pairs := make([]policy.LabelPair, 0, len(labels))
	for _, l := range labels {
		pairs = append(pairs, policy.LabelPair{Key: l.Key, Value: l.Value})
	}
	return policy.ValidateLabelSet(pairs)
}
