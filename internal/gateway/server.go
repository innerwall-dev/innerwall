package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/ca"
	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// PolicyStore reads persisted rendered policy.
type PolicyStore interface {
	// GetWorkloadPolicy returns the current rendered policy of a workload
	// with its version, or nil when none has been rendered.
	GetWorkloadPolicy(ctx context.Context, id identity.WorkloadID) (*innerwallv1.WorkloadPolicy, error)
}

// PolicyEvents delivers render announcements. The store implements it over
// the database's notification channel, so a render in any process reaches
// the replica holding the workload's stream without anything polling
// (ADR-0002).
type PolicyEvents interface {
	ListenPolicyChanges(ctx context.Context, log *slog.Logger, onReady func(), fn func(compiler.Announcement)) error
}

// Deps is what the gateway is built from. Enroll is required. Registry,
// Policies, Engine, and Events are required for the sync stream; a
// deployment that serves enrollment only, such as a test harness, may
// leave them nil, and Sync then refuses with FailedPrecondition.
type Deps struct {
	Enroll   *enroll.Service
	Registry registry.Store
	Policies PolicyStore
	Engine   *compiler.Engine
	Events   PolicyEvents
	// SyncConfig is handed to every agent in HelloAck; DefaultSyncConfig
	// when nil.
	SyncConfig *innerwallv1.SyncConfig
	Log        *slog.Logger
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

// Server implements EnrollmentService and AgentService.
type Server struct {
	innerwallv1.UnimplementedEnrollmentServiceServer
	innerwallv1.UnimplementedAgentServiceServer

	enroll     *enroll.Service
	registry   registry.Store
	policies   PolicyStore
	engine     *compiler.Engine
	events     PolicyEvents
	syncConfig *innerwallv1.SyncConfig
	sessions   *sessions
	log        *slog.Logger
	nowFn      func() time.Time
}

// New constructs the service implementation.
func New(d Deps) *Server {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.SyncConfig == nil {
		d.SyncConfig = DefaultSyncConfig()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Server{
		enroll:     d.Enroll,
		registry:   d.Registry,
		policies:   d.Policies,
		engine:     d.Engine,
		events:     d.Events,
		syncConfig: d.SyncConfig,
		sessions:   &sessions{live: map[identity.WorkloadID]*session{}},
		log:        d.Log,
		nowFn:      d.Now,
	}
}

func (s *Server) now() time.Time { return s.nowFn() }

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
	s.afterEnroll(ctx, res.Workload.ID, req.GetFacts())
	return &innerwallv1.EnrollResponse{
		WorkloadId:     res.Workload.ID.String(),
		CertificatePem: res.Credential.CertificatePEM,
		CaBundlePem:    res.Credential.BundlePEM,
		AssignedLabels: labels,
	}, nil
}

// afterEnroll records the facts reported at enrollment and renders so that
// the new workload has a durable snapshot before its first stream opens,
// and so that peers whose selectors it matches learn its addresses. A
// failure here is logged, not returned: the identity is already granted
// and persisted, and the connect path renders again if no policy exists.
func (s *Server) afterEnroll(ctx context.Context, id identity.WorkloadID, facts *innerwallv1.HostFacts) {
	if s.registry == nil || s.engine == nil {
		return
	}
	if facts != nil {
		if _, err := s.registry.RecordFacts(ctx, id, facts, s.now()); err != nil {
			s.log.Error("recording enrollment facts", "workload_id", id, "error", err)
		}
	}
	if _, err := s.engine.Render(ctx); err != nil {
		s.log.Error("rendering after enrollment", "workload_id", id, "error", err)
	}
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
	case errors.Is(err, enroll.ErrWorkloadUnknown), errors.Is(err, registry.ErrWorkloadUnknown):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, ca.ErrBadCSR):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, "enrollment failed")
	}
}
