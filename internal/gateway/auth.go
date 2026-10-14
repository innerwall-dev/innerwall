package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// enrollmentServicePrefix is the one service reachable without a client
// certificate. Every other full method name on this listener requires a
// verified workload credential; the default is deny, so a new service can
// never be exposed on the unauthenticated side by omission.
var enrollmentServicePrefix = "/" + innerwallv1.EnrollmentService_ServiceDesc.ServiceName + "/"

// renewMethod is the one method an expired workload credential may call,
// within the renewal grace (ADR-0016 as amended).
const renewMethod = innerwallv1.AgentService_RenewCredential_FullMethodName

type identityKey struct{}

// principal is what the interceptor derived from the connection
// credential: the identity, and the validity of the credential that
// carried it.
type principal struct {
	id        identity.WorkloadID
	notBefore time.Time
	notAfter  time.Time
}

// WithIdentity returns a context carrying id. Exported for tests of
// handlers; production code only ever calls it from the interceptor.
func WithIdentity(ctx context.Context, id identity.WorkloadID) context.Context {
	return context.WithValue(ctx, identityKey{}, principal{id: id})
}

// IdentityFromContext returns the workload identity the interceptor derived
// from the connection credential. Handlers on AgentService call this and
// nothing else to learn who is calling.
func IdentityFromContext(ctx context.Context) (identity.WorkloadID, bool) {
	p, ok := ctx.Value(identityKey{}).(principal)
	return p.id, ok && !p.id.IsZero()
}

// credentialValidity returns the validity of the credential the identity
// in ctx was derived from; zero when the context was not built by the
// interceptor.
func credentialValidity(ctx context.Context) (notBefore, notAfter time.Time) {
	p, _ := ctx.Value(identityKey{}).(principal)
	return p.notBefore, p.notAfter
}

// ServerTLSConfig builds the listener's TLS configuration: the control
// plane's own certificate, and client certificates verified against the
// authority bundle when presented. Presenting none is allowed at the
// transport layer because EnrollmentService is authenticated by a token;
// the interceptor decides per service whether a certificate was required.
//
// The handshake verifies the chain, the client-auth usage, and possession
// of the key, but does not judge time: a credential that has expired may
// still renew within the grace, and only the interceptor knows which
// method is being called. It then re-verifies the same chain and applies
// the enrollment domain's validity rule per method, so a credential that
// is expired, or not yet valid, is refused there with a reason the agent
// can log, rather than as an anonymous handshake failure.
func ServerTLSConfig(serverCert tls.Certificate, bundlePEM []byte) (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundlePEM) {
		return nil, errors.New("gateway: authority bundle holds no certificates")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		// RequestClientCert still proves possession: the handshake checks
		// the client's signature with the presented leaf's key. The chain
		// is verified in VerifyConnection.
		ClientAuth: tls.RequestClientCert,
		ClientCAs:  pool,
		MinVersion: tls.VersionTLS13,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return nil
			}
			_, err := verifyChain(pool, cs.PeerCertificates)
			return err
		},
	}, nil
}

// verifyChain verifies a presented chain against the authority bundle for
// client authentication, as of the leaf's own expiry: every check the
// TLS layer makes except whether the leaf has expired by now, which is
// the enrollment domain's call (enroll.Service.AdmitCredential). It
// returns the verified chain, leaf first.
func verifyChain(roots *x509.CertPool, certs []*x509.Certificate) ([]*x509.Certificate, error) {
	if roots == nil {
		return nil, errors.New("gateway: no authority bundle to verify client certificates against")
	}
	leaf := certs[0]
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}
	chains, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   leaf.NotAfter,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		return nil, fmt.Errorf("gateway: client certificate does not verify: %w", err)
	}
	return chains[0], nil
}

// authenticator applies the per-service policy. Roots is the authority
// bundle the handshake verified the chain against, and Enroll the domain
// whose validity rule decides what a credential may call now.
type authenticator struct {
	roots  *x509.CertPool
	enroll *enroll.Service
}

// authenticate applies the per-service policy to one call. It returns the
// context to run the handler with, carrying the derived identity when the
// service requires one.
func (a *authenticator) authenticate(ctx context.Context, fullMethod string) (context.Context, error) {
	if strings.HasPrefix(fullMethod, enrollmentServicePrefix) {
		return ctx, nil
	}
	chain, err := a.verifiedChain(ctx)
	if err != nil {
		return nil, err
	}
	leaf := chain[0]
	id, err := identity.FromCertificate(leaf)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, fmt.Sprintf("credential carries no workload identity: %v", err))
	}
	// The issuing chain must be valid now, whatever the leaf may do.
	for _, c := range chain[1:] {
		if err := a.enroll.AdmitCredential(c.NotBefore, c.NotAfter, false); err != nil {
			return nil, status.Error(codes.Unauthenticated, "the authority that issued this credential is outside its validity")
		}
	}
	if err := a.enroll.AdmitCredential(leaf.NotBefore, leaf.NotAfter, fullMethod == renewMethod); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}
	return context.WithValue(ctx, identityKey{}, principal{id: id, notBefore: leaf.NotBefore, notAfter: leaf.NotAfter}), nil
}

// verifiedChain re-verifies the chain the connection presented, with the
// same function the handshake ran. The handshake proved possession of
// the leaf's key; nothing here reads a certificate that was not verified.
func (a *authenticator) verifiedChain(ctx context.Context) ([]*x509.Certificate, error) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return nil, status.Error(codes.Unauthenticated, "workload credential required")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.PeerCertificates) == 0 {
		return nil, status.Error(codes.Unauthenticated, "workload credential required")
	}
	chain, err := verifyChain(a.roots, info.State.PeerCertificates)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}
	return chain, nil
}

// unary is the unary interceptor enforcing the per-service policy.
func (a *authenticator) unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, err := a.authenticate(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

// stream is the stream interceptor enforcing the per-service policy.
func (a *authenticator) stream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := a.authenticate(ss.Context(), info.FullMethod)
	if err != nil {
		return err
	}
	return handler(srv, &identityStream{ServerStream: ss, ctx: ctx})
}

type identityStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *identityStream) Context() context.Context { return s.ctx }
