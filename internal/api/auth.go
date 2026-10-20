package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/innerwall-dev/innerwall/internal/authn"
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

// sessionKey carries the identifier of the session a request authenticated
// with, kept only so the surface can end the session on logout; no handler
// reads it. The principal travels separately, through authn's context, so
// what a handler can see never includes the credential form.
type sessionKey struct{}

// authentication is the middleware in front of the routes. It reads the
// credential a request presents and resolves it through the authenticator
// to the one operator principal; how a credential is checked is the
// authenticator's, and nothing here holds it.
type authentication struct {
	authenticator authn.Authenticator
	log           *slog.Logger
}

// Middleware requires a credential for every request under the API prefix
// except the public routes. A missing, malformed, expired, or revoked
// credential is answered with a 401 problem; the response never says which
// of those it was, beyond the problem type a client needs for its login
// state. An invalid cookie is cleared so a browser stops presenting it.
func (a *authentication) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, public := publicRoutes[r.Method+" "+r.URL.Path]; public {
			next.ServeHTTP(w, r)
			return
		}
		p, sessionID, err := a.authenticate(r)
		if err != nil {
			if c, cerr := r.Cookie(SessionCookie); cerr == nil && c.Value != "" {
				http.SetCookie(w, clearSessionCookie())
			}
			a.log.Debug("operator request refused", "method", r.Method, "path", r.URL.Path, "reason", err)
			w.Header().Set("WWW-Authenticate", `Bearer realm="innerwall"`)
			writeProblem(w, problemUnauthenticated)
			return
		}
		ctx := authn.NewContext(r.Context(), p)
		if sessionID != "" {
			ctx = context.WithValue(ctx, sessionKey{}, sessionID)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

var errNoCredential = errors.New("no credential presented")

// authenticate resolves the request's credential to the principal and,
// when the credential was a session, its identifier. A bearer token, when
// present, is the credential; the cookie is consulted only when there is
// no authorization header, so automation that sends both cannot be
// downgraded to a cookie it did not mean to present.
func (a *authentication) authenticate(r *http.Request) (*authn.Principal, string, error) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		scheme, token, ok := strings.Cut(auth, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			return nil, "", operator.ErrTokenMalformed
		}
		p, err := a.authenticator.Verify(r.Context(), authn.Credential{Form: authn.OperatorToken, Secret: strings.TrimSpace(token)})
		if err != nil {
			return nil, "", err
		}
		return p, "", nil
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, "", errNoCredential
	}
	p, err := a.authenticator.Verify(r.Context(), authn.Credential{Form: authn.Session, Secret: c.Value})
	if err != nil {
		return nil, "", err
	}
	return p, c.Value, nil
}

// EndSession revokes the session the request authenticated with, if it
// was a session, and clears the cookie either way. It is the one place the
// credential form is consulted, and it is not a handler.
func (a *authentication) EndSession(ctx context.Context, w http.ResponseWriter) error {
	if id, ok := ctx.Value(sessionKey{}).(string); ok && id != "" {
		if err := a.authenticator.End(ctx, id); err != nil {
			return err
		}
	}
	http.SetCookie(w, clearSessionCookie())
	return nil
}

// sessionCookie is the cookie a login sets: unreadable by scripts, sent
// only over TLS, and SameSite=Lax, so it stays off cross-site subrequests
// and writes but does travel on a top-level cross-site navigation by a
// safe method. The defense against a cross-site write is the origin guard
// every unsafe request passes (csrf.go), not this attribute (ADR-0021).
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
