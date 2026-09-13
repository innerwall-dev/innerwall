package api

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/wrapperspb"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/operator"
)

// APIPrefix is where the façade is mounted. Every other path belongs to
// the console.
const APIPrefix = "/api/v1"

// Deps is what the operator surface is built from.
type Deps struct {
	Operators *operator.Service
	// Site is the label the console header shows; empty when none is
	// configured.
	Site string
	// LoginAttempts and LoginWindow configure the login throttle; the
	// defaults apply when zero.
	LoginAttempts int
	LoginWindow   time.Duration
	Log           *slog.Logger
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

// Server implements OperatorService and owns the listener's middleware.
type Server struct {
	innerwallv1.UnimplementedOperatorServiceServer

	operators *operator.Service
	site      string
	auth      *Authenticator
	throttle  *Throttle
	log       *slog.Logger
	nowFn     func() time.Time
}

// New constructs the surface.
func New(d Deps) *Server {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Server{
		operators: d.Operators,
		site:      d.Site,
		auth:      &Authenticator{Operators: d.Operators, Log: d.Log},
		throttle:  &Throttle{Limit: d.LoginAttempts, Window: d.LoginWindow, Now: d.Now},
		log:       d.Log,
		nowFn:     d.Now,
	}
}

// Handler builds the listener's handler: the generated façade under
// APIPrefix behind the origin guard, the login throttle, and the
// authentication middleware, in that order, and the console mount point
// for every other path.
func (s *Server) Handler() http.Handler {
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions:   protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true},
			UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: true},
		}),
		runtime.WithErrorHandler(problemErrorHandler),
		runtime.WithRoutingErrorHandler(problemRoutingErrorHandler),
		runtime.WithOutgoingHeaderMatcher(outgoingHeader),
		// Request headers carry credentials; they are the middleware's to
		// read and are not copied into handler metadata.
		runtime.WithIncomingHeaderMatcher(func(string) (string, bool) { return "", false }),
	)
	if err := innerwallv1.RegisterOperatorServiceHandlerServer(context.Background(), mux, s); err != nil {
		panic(err) // the generated registration cannot fail on a fresh mux
	}
	root := http.NewServeMux()
	root.Handle(APIPrefix+"/", CrossOriginGuard(s.throttle.Middleware(s.auth.Middleware(mux))))
	root.Handle("/", http.HandlerFunc(consoleMountPoint))
	return root
}

// outgoingHeader forwards the one response header a handler sets, the
// session cookie, under its HTTP name and drops everything else.
func outgoingHeader(key string) (string, bool) {
	if key == setCookieHeader {
		return "Set-Cookie", true
	}
	return "", false
}

// consoleMountPoint is where the embedded console is served once it
// exists (ADR-0008). Until the scaffold lands, every path outside the API
// prefix answers not found from here.
func consoleMountPoint(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, Problem{Type: ProblemNotFound, Title: http.StatusText(http.StatusNotFound), Status: http.StatusNotFound, Detail: "the operator console is not served by this build; the API is under " + APIPrefix})
}

// NewHTTPServer wraps the handler in a server with the listener's TLS
// configuration and header timeouts set.
func NewHTTPServer(tlsCfg *tls.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

func (s *Server) me(p *operator.Principal) *innerwallv1.Me {
	me := &innerwallv1.Me{Site: s.site}
	if p != nil && p.DisplayName != nil {
		me.DisplayName = wrapperspb.String(*p.DisplayName)
	}
	return me
}

// CreateSession implements OperatorService. The password is verified
// before anything else happens; a control plane with no password refuses
// with its own problem type and never offers to set one here.
func (s *Server) CreateSession(ctx context.Context, req *innerwallv1.CreateSessionRequest) (*innerwallv1.CreateSessionResponse, error) {
	err := s.operators.VerifyPassword(ctx, req.GetPassword())
	switch {
	case errors.Is(err, operator.ErrNoOperator):
		return nil, conditionError(codes.FailedPrecondition, reasonNoPassword, "no operator password has been set; run `innerwall operator set-password` on the control-plane host")
	case errors.Is(err, operator.ErrPasswordMismatch):
		return nil, conditionError(codes.Unauthenticated, reasonInvalidCredentials, "the password is not correct")
	case err != nil:
		s.log.Error("verifying operator password", "error", err)
		return nil, status.Error(codes.Internal, "verifying password")
	}
	id, sess, err := s.operators.CreateSession(ctx)
	if err != nil {
		s.log.Error("creating operator session", "error", err)
		return nil, status.Error(codes.Internal, "creating session")
	}
	if err := grpc.SetHeader(ctx, metadata.Pairs(setCookieHeader, sessionCookie(id, sess.ExpiresAt.Sub(s.nowFn())).String())); err != nil {
		return nil, status.Error(codes.Internal, "setting session cookie")
	}
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	s.log.Info("operator logged in")
	return &innerwallv1.CreateSessionResponse{Me: s.me(p)}, nil
}

// principal reads the operator for a freshly created session, which has
// not passed through the middleware.
func (s *Server) principal(ctx context.Context) (*operator.Principal, error) {
	op, err := s.operators.Store.GetOperator(ctx)
	if err != nil {
		s.log.Error("reading operator", "error", err)
		return nil, status.Error(codes.Internal, "reading operator")
	}
	return &operator.Principal{DisplayName: op.DisplayName}, nil
}

// DeleteSession implements OperatorService.
func (s *Server) DeleteSession(ctx context.Context, _ *innerwallv1.DeleteSessionRequest) (*innerwallv1.DeleteSessionResponse, error) {
	if err := s.auth.EndSession(ctx); err != nil {
		s.log.Error("ending operator session", "error", err)
		return nil, status.Error(codes.Internal, "ending session")
	}
	return &innerwallv1.DeleteSessionResponse{}, nil
}

// GetMe implements OperatorService.
func (s *Server) GetMe(ctx context.Context, _ *innerwallv1.GetMeRequest) (*innerwallv1.GetMeResponse, error) {
	p, ok := PrincipalFromContext(ctx)
	if !ok {
		// The middleware admits no unauthenticated request to this
		// handler; reaching here is a wiring fault, not a client error.
		return nil, status.Error(codes.Unauthenticated, "no principal")
	}
	return &innerwallv1.GetMeResponse{Me: s.me(p)}, nil
}
