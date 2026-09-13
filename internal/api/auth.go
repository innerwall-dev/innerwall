package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/innerwall-dev/innerwall/internal/operator"
)

// SessionCookie is the name of the cookie that carries a session identifier.
const SessionCookie = "innerwall_session"

// publicRoutes are the only requests under the API prefix served without a
// credential, named explicitly so a new endpoint cannot land on the
// unauthenticated side by omission (ADR-0021). The login endpoint is the
// whole list.
var publicRoutes = map[string]struct{}{
	http.MethodPost + " " + APIPrefix + "/session": {},
}

// authInfo is what the middleware derives from a request. The principal
// is what handlers see; the session identifier is kept only so the surface
// can end the session on logout, and no handler reads it.
type authInfo struct {
	principal *operator.Principal
	sessionID string
}

type authKey struct{}

// PrincipalFromContext returns the authenticated operator. Handlers on
// authenticated routes call this and nothing else to learn who is calling;
// it never reveals which credential form was presented.
func PrincipalFromContext(ctx context.Context) (*operator.Principal, bool) {
	info, ok := ctx.Value(authKey{}).(*authInfo)
	if !ok || info == nil || info.principal == nil {
		return nil, false
	}
	return info.principal, true
}

// Authenticator resolves a session cookie or a bearer operator token to
// the one operator principal.
type Authenticator struct {
	Operators *operator.Service
	Log       *slog.Logger
}

// Middleware requires a credential for every request under the API prefix
// except the public routes. A missing, malformed, expired, or revoked
// credential is answered with a 401 problem; the response never says which
// of those it was, beyond the problem type a client needs for its login
// state. An invalid cookie is cleared so a browser stops presenting it.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, public := publicRoutes[r.Method+" "+r.URL.Path]; public {
			next.ServeHTTP(w, r)
			return
		}
		info, err := a.authenticate(r)
		if err != nil {
			if c, cerr := r.Cookie(SessionCookie); cerr == nil && c.Value != "" {
				http.SetCookie(w, clearSessionCookie())
			}
			a.log().Debug("operator request refused", "method", r.Method, "path", r.URL.Path, "reason", err)
			w.Header().Set("WWW-Authenticate", `Bearer realm="innerwall"`)
			writeProblem(w, Problem{Type: ProblemUnauthenticated, Title: "Authentication required", Status: http.StatusUnauthorized})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authKey{}, info)))
	})
}

var errNoCredential = errors.New("no credential presented")

// authenticate resolves the request's credential. A bearer token, when
// present, is the credential; the cookie is consulted only when there is
// no authorization header, so automation that sends both cannot be
// downgraded to a cookie it did not mean to present.
func (a *Authenticator) authenticate(r *http.Request) (*authInfo, error) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		scheme, token, ok := strings.Cut(auth, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			return nil, operator.ErrTokenMalformed
		}
		p, err := a.Operators.ValidateToken(r.Context(), strings.TrimSpace(token))
		if err != nil {
			return nil, err
		}
		return &authInfo{principal: p}, nil
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, errNoCredential
	}
	p, err := a.Operators.ValidateSession(r.Context(), c.Value)
	if err != nil {
		return nil, err
	}
	return &authInfo{principal: p, sessionID: c.Value}, nil
}

// EndSession revokes the session the request authenticated with, if it
// was a session, and clears the cookie either way. It is the one place the
// credential form is consulted, and it is not a handler.
func (a *Authenticator) EndSession(ctx context.Context) error {
	if info, ok := ctx.Value(authKey{}).(*authInfo); ok && info.sessionID != "" {
		if err := a.Operators.DeleteSession(ctx, info.sessionID); err != nil {
			return err
		}
	}
	return grpc.SetHeader(ctx, metadata.Pairs(setCookieHeader, clearSessionCookie().String()))
}

func (a *Authenticator) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

// setCookieHeader is the metadata key a handler sets a cookie through; the
// outgoing header matcher turns it into the HTTP header.
const setCookieHeader = "set-cookie"

// sessionCookie is the cookie a login sets: unreadable by scripts, sent
// only over TLS, and not sent on cross-site navigations (ADR-0021).
func sessionCookie(id string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   int(ttl / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

func clearSessionCookie() *http.Cookie {
	c := sessionCookie("", 0)
	c.MaxAge = -1
	return c
}
