package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
)

// APIPrefix is where the surface is mounted. Every other path belongs to
// the console.
const APIPrefix = "/api/v1"

// maxBodyBytes bounds a request body; the surface's requests are small
// documents, and a bound keeps a client from holding a handler on a stream.
const maxBodyBytes = 64 << 10

// Deps is what the operator surface is built from.
type Deps struct {
	Operators *operator.Service
	// Reads is the read model the read endpoints call.
	Reads *readmodel.Reader
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

// Server holds the surface's handlers and the listener's middleware. The
// handlers are hand-shaped over the domain layer (ADR-0007 as amended):
// each carries the transport semantics of its endpoint and calls the same
// domain functions the command line calls, holding no logic of its own.
type Server struct {
	operators *operator.Service
	reads     *readmodel.Reader
	site      string
	auth      *Authenticator
	throttle  *Throttle
	log       *slog.Logger
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
		reads:     d.Reads,
		site:      d.Site,
		auth:      &Authenticator{Operators: d.Operators, Log: d.Log},
		throttle:  &Throttle{Limit: d.LoginAttempts, Window: d.LoginWindow, Now: d.Now},
		log:       d.Log,
	}
}

// Handler builds the listener's handler: the routes under APIPrefix behind
// the origin guard, the login throttle, and the authentication middleware,
// in that order, and the console mount point for every other path.
func (s *Server) Handler() http.Handler {
	routes := newRouter()
	routes.handle(http.MethodPost, APIPrefix+"/session", s.createSession)
	routes.handle(http.MethodDelete, APIPrefix+"/session", s.deleteSession)
	routes.handle(http.MethodGet, APIPrefix+"/me", s.getMe)
	routes.handle(http.MethodGet, APIPrefix+"/flows/rollup", s.getFlowsRollup)
	routes.handle(http.MethodGet, APIPrefix+"/flows", s.getFlows)
	routes.handle(http.MethodGet, APIPrefix+"/workloads", s.getWorkloads)
	routes.handle(http.MethodGet, APIPrefix+"/workloads/{id}", s.getWorkload)
	routes.handle(http.MethodGet, APIPrefix+"/workloads/{id}/rendered-policy", s.getRenderedPolicy)

	root := http.NewServeMux()
	root.Handle(APIPrefix+"/", CrossOriginGuard(s.throttle.Middleware(s.auth.Middleware(routes))))
	root.Handle("/", http.HandlerFunc(consoleMountPoint))
	return root
}

// router matches a method and a path against fixed patterns. A pattern
// is a path whose segments are literals or one-segment parameters written
// {name}; a literal segment outranks a parameter at the same position, so
// registration order does not matter. It answers a known path with an
// unsupported method with 405 and an Allow header, and anything else with
// 404, both as problem documents; the standard multiplexer answers both in
// plain text, which the surface never speaks.
type router struct {
	routes []*route
}

type route struct {
	pattern  string
	segments []string
	methods  map[string]http.HandlerFunc
}

func newRouter() *router { return &router{} }

func (r *router) handle(method, pattern string, h http.HandlerFunc) {
	for _, rt := range r.routes {
		if rt.pattern == pattern {
			rt.methods[method] = h
			return
		}
	}
	r.routes = append(r.routes, &route{pattern: pattern, segments: strings.Split(pattern, "/"), methods: map[string]http.HandlerFunc{method: h}})
}

type pathParamsKey struct{}

// pathParam returns the value the {name} segment matched, or "".
func pathParam(r *http.Request, name string) string {
	params, _ := r.Context().Value(pathParamsKey{}).(map[string]string)
	return params[name]
}

// match reports whether path fits the route and the parameters it binds.
func (rt *route) match(path string) (map[string]string, bool) {
	segments := strings.Split(path, "/")
	if len(segments) != len(rt.segments) {
		return nil, false
	}
	var params map[string]string
	for i, want := range rt.segments {
		got := segments[i]
		if strings.HasPrefix(want, "{") && strings.HasSuffix(want, "}") {
			if got == "" {
				return nil, false
			}
			if params == nil {
				params = map[string]string{}
			}
			params[want[1:len(want)-1]] = got
			continue
		}
		if got != want {
			return nil, false
		}
	}
	return params, true
}

// literalness counts the literal segments of a route, so that the most
// specific match wins.
func (rt *route) literalness() int {
	n := 0
	for _, s := range rt.segments {
		if !strings.HasPrefix(s, "{") {
			n++
		}
	}
	return n
}

func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var best *route
	var params map[string]string
	for _, rt := range r.routes {
		p, ok := rt.match(req.URL.Path)
		if !ok {
			continue
		}
		if best == nil || rt.literalness() > best.literalness() {
			best, params = rt, p
		}
	}
	if best == nil {
		writeProblem(w, problemNotFound)
		return
	}
	if h, ok := best.methods[req.Method]; ok {
		if params != nil {
			req = req.WithContext(context.WithValue(req.Context(), pathParamsKey{}, params))
		}
		h(w, req)
		return
	}
	allowed := make([]string, 0, len(best.methods))
	for m := range best.methods {
		allowed = append(allowed, m)
	}
	sort.Strings(allowed)
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeProblem(w, Problem{Type: ProblemMethodNotAllowed, Title: http.StatusText(http.StatusMethodNotAllowed), Status: http.StatusMethodNotAllowed})
}

// consoleMountPoint is where the embedded console is served once it
// exists (ADR-0008). Until the scaffold lands, every path outside the API
// prefix answers not found from here.
func consoleMountPoint(w http.ResponseWriter, _ *http.Request) {
	p := problemNotFound
	p.Detail = "the operator console is not served by this build; the API is under " + APIPrefix
	writeProblem(w, p)
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

// Me is the operator as the console sees it: the display name set with
// the password (null until one is set, and the console renders a generic
// fallback) and the site label configured on the control plane (empty
// when none is).
type Me struct {
	DisplayName *string `json:"display_name"`
	Site        string  `json:"site"`
}

func (s *Server) me(p *operator.Principal) Me {
	me := Me{Site: s.site}
	if p != nil {
		me.DisplayName = p.DisplayName
	}
	return me
}

// decodeJSON reads one JSON document into v. A body that is not JSON, or
// that is more than one document, is an invalid request; unknown fields
// are ignored so a newer console can talk to an older control plane.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(v); err != nil {
		writeProblem(w, Problem{Type: ProblemInvalidRequest, Title: "Invalid request body", Status: http.StatusBadRequest, Detail: "the body is not a JSON document of the expected shape"})
		return false
	}
	if dec.More() {
		writeProblem(w, Problem{Type: ProblemInvalidRequest, Title: "Invalid request body", Status: http.StatusBadRequest, Detail: "the body holds more than one document"})
		return false
	}
	return true
}

// createSession is POST /api/v1/session: exchange the password for a
// session. The password is verified before anything else happens; a
// control plane with no password refuses with its own problem type and
// never offers to set one here.
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	err := s.operators.VerifyPassword(ctx, req.Password)
	switch {
	case errors.Is(err, operator.ErrNoOperator):
		writeProblem(w, Problem{Type: ProblemNoPassword, Title: "No operator password has been set", Status: http.StatusForbidden, Detail: "no operator password has been set; run `innerwall operator set-password` on the control-plane host"})
		return
	case errors.Is(err, operator.ErrPasswordMismatch):
		w.Header().Set("WWW-Authenticate", `Bearer realm="innerwall"`)
		writeProblem(w, Problem{Type: ProblemInvalidCredentials, Title: "Invalid credentials", Status: http.StatusUnauthorized, Detail: "the password is not correct"})
		return
	case err != nil:
		s.log.Error("verifying operator password", "error", err)
		writeProblem(w, problemInternal)
		return
	}
	id, sess, err := s.operators.CreateSession(ctx)
	if err != nil {
		s.log.Error("creating operator session", "error", err)
		writeProblem(w, problemInternal)
		return
	}
	op, err := s.operators.Store.GetOperator(ctx)
	if err != nil {
		s.log.Error("reading operator", "error", err)
		writeProblem(w, problemInternal)
		return
	}
	http.SetCookie(w, sessionCookie(id, sess.ExpiresAt.Sub(sess.CreatedAt)))
	s.log.Info("operator logged in")
	writeJSON(w, s.me(&operator.Principal{DisplayName: op.DisplayName}))
}

// deleteSession is DELETE /api/v1/session: revoke the current session and
// clear the cookie.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.EndSession(r.Context(), w); err != nil {
		s.log.Error("ending operator session", "error", err)
		writeProblem(w, problemInternal)
		return
	}
	writeJSON(w, struct{}{})
}

// getMe is GET /api/v1/me: the authenticated operator and the site label.
func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		// The middleware admits no unauthenticated request to this
		// handler; reaching here is a wiring fault, not a client error.
		writeProblem(w, problemUnauthenticated)
		return
	}
	writeJSON(w, s.me(p))
}
