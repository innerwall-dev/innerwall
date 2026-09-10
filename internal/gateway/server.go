package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Server implements EnrollmentService and AgentService.
type Server struct {
	innerwallv1.UnimplementedEnrollmentServiceServer
	innerwallv1.UnimplementedAgentServiceServer

	enroll *enroll.Service
	log    *slog.Logger
}

// New constructs the service implementation.
func New(svc *enroll.Service, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{enroll: svc, log: log}
}

// NewGRPCServer builds a gRPC server with the listener's TLS configuration
// and the per-service authentication interceptors, and registers both
// services on it.
func NewGRPCServer(tlsCfg *tls.Config, srv *Server, opts ...grpc.ServerOption) *grpc.Server {
	opts = append(opts,
		grpc.Creds(credentials.NewTLS(tlsCfg)),
		grpc.ChainUnaryInterceptor(UnaryAuth),
		grpc.ChainStreamInterceptor(StreamAuth),
	)
	g := grpc.NewServer(opts...)
	innerwallv1.RegisterEnrollmentServiceServer(g, srv)
	innerwallv1.RegisterAgentServiceServer(g, srv)
	return g
}

// Enroll implements EnrollmentService. The token is the credential; no
// identity is read from the connection.
func (s *Server) Enroll(ctx context.Context, req *innerwallv1.EnrollRequest) (*innerwallv1.EnrollResponse, error) {
	hostname := ""
	if req.GetFacts() != nil {
		hostname = req.GetFacts().GetHostname()
	}
	res, err := s.enroll.Enroll(ctx, req.GetProvisioningToken(), req.GetCsrPem(), hostname)
	if err != nil {
		// The token plaintext is never logged; the error carries none of it.
		s.log.Info("enrollment refused", "hostname", hostname, "error", err)
		return nil, toStatus(err)
	}
	labels := make([]*innerwallv1.Label, 0, len(res.Workload.Labels))
	for _, l := range res.Workload.Labels {
		labels = append(labels, &innerwallv1.Label{Key: l.Key, Value: l.Value})
	}
	s.log.Info("workload enrolled", "workload_id", res.Workload.ID, "hostname", hostname, "token_id", res.Workload.TokenID)
	return &innerwallv1.EnrollResponse{
		WorkloadId:     res.Workload.ID.String(),
		CertificatePem: res.Credential.CertificatePEM,
		CaBundlePem:    res.Credential.BundlePEM,
		AssignedLabels: labels,
	}, nil
}

// RenewCredential implements AgentService. The workload being renewed is
// whatever the interceptor derived from the connection credential.
func (s *Server) RenewCredential(ctx context.Context, req *innerwallv1.RenewCredentialRequest) (*innerwallv1.RenewCredentialResponse, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		// Unreachable when the interceptor is installed; kept so a handler
		// registered without it fails closed rather than renewing nobody.
		return nil, status.Error(codes.Unauthenticated, "workload credential required")
	}
	cred, err := s.enroll.Renew(ctx, id, req.GetCsrPem())
	if err != nil {
		s.log.Info("renewal refused", "workload_id", id, "error", err)
		return nil, toStatus(err)
	}
	s.log.Info("credential renewed", "workload_id", id, "serial", cred.Serial)
	return &innerwallv1.RenewCredentialResponse{
		CertificatePem: cred.CertificatePEM,
		CaBundlePem:    cred.BundlePEM,
	}, nil
}

// toStatus maps domain errors to gRPC status codes. Token failures are all
// Unauthenticated with a precise message: the caller minted the token and
// deserves to know why it no longer works, while an attacker guessing
// tokens learns nothing from "unknown" it did not already know.
func toStatus(err error) error {
	switch {
	case errors.Is(err, enroll.ErrTokenMalformed),
		errors.Is(err, enroll.ErrTokenUnknown),
		errors.Is(err, enroll.ErrTokenExpired),
		errors.Is(err, enroll.ErrTokenRevoked):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, enroll.ErrWorkloadUnknown):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, ca.ErrBadCSR):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, "enrollment failed")
	}
}
