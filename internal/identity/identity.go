// Package identity is the single definition of workload identity as it
// appears in credentials. A workload identity is a control-plane-assigned
// UUID; in an issued certificate it is carried as exactly one URI
// subject alternative name of the form
//
//	innerwall://workload/<uuid>
//
// Every component that formats or parses that URI goes through this package.
// Nothing else in the repository builds or inspects the string, so the
// representation can only ever change in one place (ADR-0016).
package identity

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

// Scheme is the URI scheme of a workload identity.
const Scheme = "innerwall"

// hostWorkload is the URI host that names the workload namespace. The host
// component is a namespace, not a network location: it lets the same scheme
// name other kinds of principals later (an operator, a control-plane replica)
// without ambiguity.
const hostWorkload = "workload"

// ErrNoIdentity is returned when a certificate carries no workload identity.
var ErrNoIdentity = errors.New("identity: certificate carries no workload identity")

// ErrAmbiguousIdentity is returned when a certificate carries more than one
// URI SAN. A credential names exactly one principal; anything else is a
// malformed credential, not a choice to make.
var ErrAmbiguousIdentity = errors.New("identity: certificate carries more than one URI SAN")

// WorkloadID is the control-plane-assigned identity of a workload.
type WorkloadID struct {
	id uuid.UUID
}

// NewWorkloadID generates a fresh workload identity. Version 7 UUIDs are
// time-ordered, which keeps the primary key of the workloads table append
// friendly without revealing anything an enrollee could influence.
func NewWorkloadID() (WorkloadID, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return WorkloadID{}, fmt.Errorf("identity: generating workload id: %w", err)
	}
	return WorkloadID{id: u}, nil
}

// ParseWorkloadID parses the canonical textual UUID form.
func ParseWorkloadID(s string) (WorkloadID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return WorkloadID{}, fmt.Errorf("identity: parsing workload id %q: %w", s, err)
	}
	if u == uuid.Nil {
		return WorkloadID{}, errors.New("identity: workload id must not be the nil uuid")
	}
	return WorkloadID{id: u}, nil
}

// FromUUID wraps an existing UUID, for values read back from the store.
func FromUUID(u uuid.UUID) WorkloadID {
	return WorkloadID{id: u}
}

// UUID returns the underlying UUID, for the store.
func (w WorkloadID) UUID() uuid.UUID { return w.id }

// String returns the canonical textual UUID form.
func (w WorkloadID) String() string { return w.id.String() }

// IsZero reports whether the identity is unset.
func (w WorkloadID) IsZero() bool { return w.id == uuid.Nil }

// URI returns the identity as the URI carried in a certificate SAN.
func (w WorkloadID) URI() *url.URL {
	return &url.URL{Scheme: Scheme, Host: hostWorkload, Path: "/" + w.id.String()}
}

// ParseURI parses a workload identity URI. It accepts exactly the form
// produced by URI: any other scheme, host, path shape, query, fragment, or
// user information is rejected rather than tolerated.
func ParseURI(u *url.URL) (WorkloadID, error) {
	if u == nil {
		return WorkloadID{}, errors.New("identity: nil uri")
	}
	if u.Scheme != Scheme {
		return WorkloadID{}, fmt.Errorf("identity: uri %q: scheme must be %q", u, Scheme)
	}
	if u.Host != hostWorkload || u.User != nil || u.Opaque != "" {
		return WorkloadID{}, fmt.Errorf("identity: uri %q: expected %s://%s/<uuid>", u, Scheme, hostWorkload)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return WorkloadID{}, fmt.Errorf("identity: uri %q: query and fragment are not allowed", u)
	}
	if len(u.Path) < 2 || u.Path[0] != '/' {
		return WorkloadID{}, fmt.Errorf("identity: uri %q: expected %s://%s/<uuid>", u, Scheme, hostWorkload)
	}
	raw := u.Path[1:]
	if len(raw) != 36 {
		// uuid.Parse also accepts braced, URN-prefixed, and hex forms. A
		// credential has one canonical spelling.
		return WorkloadID{}, fmt.Errorf("identity: uri %q: workload id is not a canonical uuid", u)
	}
	return ParseWorkloadID(raw)
}

// ParseURIString parses the textual form of a workload identity URI.
func ParseURIString(s string) (WorkloadID, error) {
	u, err := url.Parse(s)
	if err != nil {
		return WorkloadID{}, fmt.Errorf("identity: parsing uri: %w", err)
	}
	return ParseURI(u)
}

// FromCertificate extracts the workload identity a certificate binds. The
// certificate must carry exactly one URI SAN and it must parse; the subject,
// common name, and every other field are ignored, because the URI SAN is the
// only place identity lives (ADR-0016).
func FromCertificate(cert *x509.Certificate) (WorkloadID, error) {
	if cert == nil {
		return WorkloadID{}, ErrNoIdentity
	}
	switch len(cert.URIs) {
	case 0:
		return WorkloadID{}, ErrNoIdentity
	case 1:
		return ParseURI(cert.URIs[0])
	default:
		return WorkloadID{}, ErrAmbiguousIdentity
	}
}
