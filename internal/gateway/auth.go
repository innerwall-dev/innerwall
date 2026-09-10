package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// enrollmentServicePrefix is the one service reachable without a client
// certificate. Every other full method name on this listener requires a
// verified workload credential; the default is deny, so a new service can
// never be exposed on the unauthenticated side by omission.
var enrollmentServicePrefix = "/" + innerwallv1.EnrollmentService_ServiceDesc.ServiceName + "/"

type identityKey struct{}

// WithIdentity returns a context carrying id. Exported for tests of
// handlers; production code only ever calls it from the interceptor.
func WithIdentity(ctx context.Context, id identity.WorkloadID) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFromContext returns the workload identity the interceptor derived
// from the connection credential. Handlers on AgentService call this and
// nothing else to learn who is calling.
func IdentityFromContext(ctx context.Context) (identity.WorkloadID, bool) {
	id, ok := ctx.Value(identityKey{}).(identity.WorkloadID)
	return id, ok && !id.IsZero()
}

// ServerTLSConfig builds the listener's TLS configuration: the control
// plane's own certificate, and client certificates verified against the
// authority bundle when presented. Presenting none is allowed at the
// transport layer because EnrollmentService is authenticated by a token;
// the interceptor decides per service whether a certificate was required.
func ServerTLSConfig(serverCert tls.Certificate, bundlePEM []byte) (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundlePEM) {
		return nil, errors.New("gateway: authority bundle holds no certificates")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// authenticate applies the per-service policy to one call. It returns the
// context to run the handler with, carrying the derived identity when the
// service requires one.
func authenticate(ctx context.Context, fullMethod string) (context.Context, error) {
	if strings.HasPrefix(fullMethod, enrollmentServicePrefix) {
		return ctx, nil
	}
	id, err := identityFromPeer(ctx)
	if err != nil {
		return nil, err
	}
	return WithIdentity(ctx, id), nil
}

// identityFromPeer derives the workload identity from the verified chain of
// the connection. Only the chain the TLS layer verified against the
// authority bundle counts; the raw presented certificates are never read.
func identityFromPeer(ctx context.Context) (identity.WorkloadID, error) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return identity.WorkloadID{}, status.Error(codes.Unauthenticated, "workload credential required")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return identity.WorkloadID{}, status.Error(codes.Unauthenticated, "workload credential required")
	}
	if len(info.State.VerifiedChains) == 0 || len(info.State.VerifiedChains[0]) == 0 {
		return identity.WorkloadID{}, status.Error(codes.Unauthenticated, "workload credential required")
	}
	id, err := identity.FromCertificate(info.State.VerifiedChains[0][0])
	if err != nil {
		return identity.WorkloadID{}, status.Error(codes.Unauthenticated, fmt.Sprintf("credential carries no workload identity: %v", err))
	}
	return id, nil
}

// UnaryAuth is the unary interceptor enforcing the per-service policy.
func UnaryAuth(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, err := authenticate(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

// StreamAuth is the stream interceptor enforcing the per-service policy.
func StreamAuth(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := authenticate(ss.Context(), info.FullMethod)
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
