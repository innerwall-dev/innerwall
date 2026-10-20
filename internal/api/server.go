package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/innerwall-dev/innerwall/internal/authn"
	"github.com/innerwall-dev/innerwall/internal/buildinfo"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/policy"
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
	// Authenticator resolves the credentials the surface is presented,
	// behind the login endpoint and the authentication middleware.
	Authenticator authn.Authenticator
	// Operators is the operator domain, behind operator token management.
	Operators *operator.Service
	// Reads is the read model the read endpoints call.
	Reads *readmodel.Reader
	// Authoring is the authored model's write domain, behind the
	// ruleset, rule, service, and address group endpoints.
	Authoring *policy.Authoring
	// Fleet is the fleet write domain, behind label edits, mode
	// changes, the selector preview, the dry run, and the directed
	// reconnect.
	Fleet *fleet.Service
	// Enroll is the enrollment domain, behind provisioning token
	// management.
	Enroll *enroll.Service
	// Site is the label the console header shows; empty when none is
	// configured.
	Site string
	// GatewayAddress is the host:port agents reach the agent gateway at,
	// as the operator configured it for the console's enroll command;
	// empty when none is configured. It is never derived from the bind
	// address or the host's interfaces.
	GatewayAddress string
	// Console is the built console tree served at every path outside the
	// API prefix; nil when this build carries none (ADR-0008).
	Console fs.FS
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
	operators     *operator.Service
	reads         *readmodel.Reader
	authoring     *policy.Authoring
	fleet         *fleet.Service
	enroll        *enroll.Service
	site          string
	gateway       string
	console       fs.FS
	authenticator authn.Authenticator
	auth          *authentication
	throttle      *Throttle
	log           *slog.Logger
	now           func() time.Time
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
		operators:     d.Operators,
		reads:         d.Reads,
		authoring:     d.Authoring,
		fleet:         d.Fleet,
		enroll:        d.Enroll,
		site:          d.Site,
		gateway:       d.GatewayAddress,
		console:       d.Console,
		authenticator: d.Authenticator,
		auth:          &authentication{authenticator: d.Authenticator, log: d.Log},
		throttle:      &Throttle{Limit: d.LoginAttempts, Window: d.LoginWindow, Now: d.Now},
		log:           d.Log,
		now:           d.Now,
	}
}

// Surface is the composition of the operator listener's handler: the
// middleware in front of the API routes, the routes under APIPrefix, and
// the console at every other path. The listener's handler is built from it
// and from nothing else, so what the surface serves, and in what order its
// middleware runs, is read off this structure.
type Surface struct {
	// Middleware wraps the API routes, outermost first.
	Middleware []Middleware
	// Routes are the endpoints under APIPrefix.
	Routes []Route
	// Console serves every path outside APIPrefix.
	Console http.Handler
}

// Middleware is one named layer in front of the API routes.
type Middleware struct {
	Name string
	Wrap func(http.Handler) http.Handler
}

// Route is one endpoint: a method and a path pattern whose segments are
// literals or one-segment parameters written {name}.
type Route struct {
	Method  string
	Pattern string
	Handler http.HandlerFunc
}

// Use appends a middleware inside those already added.
func (sf *Surface) Use(m Middleware) { sf.Middleware = append(sf.Middleware, m) }

// Handle registers an endpoint.
func (sf *Surface) Handle(method, pattern string, h http.HandlerFunc) {
	sf.Routes = append(sf.Routes, Route{Method: method, Pattern: pattern, Handler: h})
}

func (sf *Surface) router() *router {
	r := newRouter()
	for _, rt := range sf.Routes {
		r.handle(rt.Method, rt.Pattern, rt.Handler)
	}
	return r
}

// Handler builds the listener's handler: the routes behind the middleware,
// outermost first, under APIPrefix, and the console at every other path.
func (sf *Surface) Handler() http.Handler {
	var routes http.Handler = sf.router()
	for i := len(sf.Middleware) - 1; i >= 0; i-- {
		routes = sf.Middleware[i].Wrap(routes)
	}
	root := http.NewServeMux()
	root.Handle(APIPrefix+"/", routes)
	root.Handle("/", sf.Console)
	return root
}

// RouteNames lists every method and path pattern registered, in the form
// "METHOD /api/v1/path/{param}", sorted.
func (sf *Surface) RouteNames() []string {
	var out []string
	for _, rt := range sf.router().routes {
		for m := range rt.methods {
			out = append(out, m+" "+rt.pattern)
		}
	}
	sort.Strings(out)
	return out
}

// Surface composes the operator surface from the server's handlers: the
// origin guard, the login throttle, and the authentication middleware, in
// that order, in front of every route, and the console at every other
// path.
func (s *Server) Surface() *Surface {
	sf := &Surface{Console: ConsoleHandler(s.console)}
	sf.Use(Middleware{Name: "origin guard", Wrap: CrossOriginGuard})
	sf.Use(Middleware{Name: "login throttle", Wrap: s.throttle.Middleware})
	sf.Use(Middleware{Name: "authentication", Wrap: s.auth.Middleware})
	s.mount(sf)
	return sf
}

// Handler builds the listener's handler from the server's composition.
func (s *Server) Handler() http.Handler { return s.Surface().Handler() }

// mount registers every route of the surface.
func (s *Server) mount(sf *Surface) {
	sf.Handle(http.MethodPost, APIPrefix+"/session", s.createSession)
	sf.Handle(http.MethodDelete, APIPrefix+"/session", s.deleteSession)
	sf.Handle(http.MethodGet, APIPrefix+"/me", s.getMe)
	sf.Handle(http.MethodGet, APIPrefix+"/flows/rollup", s.getFlowsRollup)
	sf.Handle(http.MethodGet, APIPrefix+"/flows/gaps", s.getFlowGaps)
	sf.Handle(http.MethodGet, APIPrefix+"/flows", s.getFlows)
	sf.Handle(http.MethodGet, APIPrefix+"/workloads", s.getWorkloads)
	sf.Handle(http.MethodGet, APIPrefix+"/workloads/counts", s.getWorkloadCounts)
	sf.Handle(http.MethodGet, APIPrefix+"/workloads/{id}", s.getWorkload)
	sf.Handle(http.MethodGet, APIPrefix+"/workloads/{id}/rendered-policy", s.getRenderedPolicy)
	sf.Handle(http.MethodGet, APIPrefix+"/workloads/{id}/labels", s.getWorkloadLabels)
	sf.Handle(http.MethodPut, APIPrefix+"/workloads/{id}/labels", s.putWorkloadLabels)
	sf.Handle(http.MethodPost, APIPrefix+"/workloads/{id}/resend-snapshot", s.resendSnapshot)
	sf.Handle(http.MethodGet, APIPrefix+"/rulesets", s.listRulesets)
	sf.Handle(http.MethodPost, APIPrefix+"/rulesets", s.createRuleset)
	sf.Handle(http.MethodGet, APIPrefix+"/rulesets/{id}", s.getRuleset)
	sf.Handle(http.MethodPut, APIPrefix+"/rulesets/{id}", s.putRuleset)
	sf.Handle(http.MethodDelete, APIPrefix+"/rulesets/{id}", s.deleteRuleset)
	sf.Handle(http.MethodPost, APIPrefix+"/rulesets/{id}/rules", s.createRule)
	sf.Handle(http.MethodGet, APIPrefix+"/rulesets/{id}/rules/{rule_id}", s.getRule)
	sf.Handle(http.MethodPut, APIPrefix+"/rulesets/{id}/rules/{rule_id}", s.putRule)
	sf.Handle(http.MethodDelete, APIPrefix+"/rulesets/{id}/rules/{rule_id}", s.deleteRule)
	sf.Handle(http.MethodGet, APIPrefix+"/services", s.listServices)
	sf.Handle(http.MethodPost, APIPrefix+"/services", s.createService)
	sf.Handle(http.MethodGet, APIPrefix+"/services/{id}", s.getService)
	sf.Handle(http.MethodPut, APIPrefix+"/services/{id}", s.putService)
	sf.Handle(http.MethodDelete, APIPrefix+"/services/{id}", s.deleteService)
	sf.Handle(http.MethodGet, APIPrefix+"/address-groups", s.listAddressGroups)
	sf.Handle(http.MethodPost, APIPrefix+"/address-groups", s.createAddressGroup)
	sf.Handle(http.MethodGet, APIPrefix+"/address-groups/{id}", s.getAddressGroup)
	sf.Handle(http.MethodPut, APIPrefix+"/address-groups/{id}", s.putAddressGroup)
	sf.Handle(http.MethodDelete, APIPrefix+"/address-groups/{id}", s.deleteAddressGroup)
	sf.Handle(http.MethodPost, APIPrefix+"/mode-changes", s.createModeChange)
	sf.Handle(http.MethodPost, APIPrefix+"/selectors/preview", s.previewSelector)
	sf.Handle(http.MethodPost, APIPrefix+"/policies/render-dryrun", s.renderDryRun)
	sf.Handle(http.MethodGet, APIPrefix+"/provisioning-tokens", s.listProvisioningTokens)
	sf.Handle(http.MethodPost, APIPrefix+"/provisioning-tokens", s.mintProvisioningToken)
	sf.Handle(http.MethodDelete, APIPrefix+"/provisioning-tokens/{id}", s.revokeProvisioningToken)
	sf.Handle(http.MethodGet, APIPrefix+"/operator-tokens", s.listOperatorTokens)
	sf.Handle(http.MethodPost, APIPrefix+"/operator-tokens", s.mintOperatorToken)
	sf.Handle(http.MethodDelete, APIPrefix+"/operator-tokens/{id}", s.revokeOperatorToken)
}

// Routes lists every method and path pattern the surface serves, in the
// form "METHOD /api/v1/path/{param}", so the hand-authored contract can be
// checked against what is actually mounted.
func (s *Server) Routes() []string { return s.Surface().RouteNames() }

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
// fallback), the site label configured on the control plane (empty when
// none is), the advertised agent gateway address (null when none is
// configured, and the console keeps its placeholder), and the version of
// the control plane answering, as internal/buildinfo names it. Only an
// authenticated request reaches it, so the version is never disclosed
// before login.
type Me struct {
	DisplayName    *string `json:"display_name"`
	Site           string  `json:"site"`
	GatewayAddress *string `json:"gateway_address"`
	Version        string  `json:"version"`
}

func (s *Server) me(p *authn.Principal) Me {
	me := Me{Site: s.site, GatewayAddress: optionalString(s.gateway), Version: buildinfo.Version()}
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
	p, err := s.authenticator.Verify(ctx, authn.Credential{Form: authn.Password, Secret: req.Password})
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
	sess, err := s.authenticator.Mint(ctx)
	if err != nil {
		s.log.Error("creating operator session", "error", err)
		writeProblem(w, problemInternal)
		return
	}
	http.SetCookie(w, sessionCookie(sess.ID, sess.Lifetime))
	s.log.Info("operator logged in")
	writeJSON(w, s.me(p))
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

// getMe is GET /api/v1/me: the authenticated operator, the site label,
// the advertised agent gateway address, and the control plane's version.
func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	p, ok := authn.FromContext(r.Context())
	if !ok {
		// The middleware admits no unauthenticated request to this
		// handler; reaching here is a wiring fault, not a client error.
		writeProblem(w, problemUnauthenticated)
		return
	}
	writeJSON(w, s.me(p))
}
