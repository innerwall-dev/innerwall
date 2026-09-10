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
)

// DefaultLeafTTL is the lifetime of an issued workload credential when the
// operator sets none. Short lifetimes replace revocation machinery: a
// credential that is not renewed simply stops working (ADR-0016).
const DefaultLeafTTL = 24 * time.Hour

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
	// RecordRenewal stores the serial and expiry of a renewed credential. It
	// returns ErrWorkloadUnknown when the id is not registered.
	RecordRenewal(ctx context.Context, id identity.WorkloadID, serial string, expiresAt, now time.Time) error
}

// Service performs enrollment and renewal.
type Service struct {
	Store     Store
	Authority ca.Authority
	// LeafTTL is the lifetime of issued credentials; DefaultLeafTTL if zero.
	LeafTTL time.Duration
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

// Credential is what an enrollment or renewal hands back.
type Credential struct {
	CertificatePEM []byte
	BundlePEM      []byte
	Serial         string
	ExpiresAt      time.Time
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
	tok = Token{
		ID:        uuid.New(),
		Hash:      hash,
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
// derived from a verified connection credential. The same identity is
// bound; the CSR contributes only its (possibly new) public key.
func (s *Service) Renew(ctx context.Context, id identity.WorkloadID, csrPEM []byte) (*Credential, error) {
	if _, err := s.Store.GetWorkload(ctx, id); err != nil {
		return nil, err
	}
	cred, err := s.issue(ctx, id, csrPEM)
	if err != nil {
		return nil, err
	}
	if err := s.Store.RecordRenewal(ctx, id, cred.Serial, cred.ExpiresAt, s.now()); err != nil {
		return nil, err
	}
	return cred, nil
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
		Serial:         cert.SerialNumber.Text(16),
		ExpiresAt:      cert.NotAfter,
	}, nil
}

func validateLabels(labels []Label) error {
	seen := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		if l.Key == "" {
			return errors.New("enroll: label key must not be empty")
		}
		if _, dup := seen[l.Key]; dup {
			return fmt.Errorf("enroll: duplicate label key %q", l.Key)
		}
		seen[l.Key] = struct{}{}
	}
	return nil
}
