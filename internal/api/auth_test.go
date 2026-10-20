package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/authn"
)

// recordingAuthenticator accepts one secret per form and records every
// credential it is asked to verify and every session it is asked to end.
type recordingAuthenticator struct {
	accept   map[authn.Form]string
	verified []authn.Credential
	ended    []string
}

func (a *recordingAuthenticator) Verify(_ context.Context, c authn.Credential) (*authn.Principal, error) {
	a.verified = append(a.verified, c)
	if want, ok := a.accept[c.Form]; ok && want == c.Secret {
		name := "Ada"
		return &authn.Principal{DisplayName: &name}, nil
	}
	return nil, errors.New("refused")
}

func (a *recordingAuthenticator) Mint(context.Context) (authn.MintedSession, error) {
	return authn.MintedSession{}, errors.New("not minting")
}

func (a *recordingAuthenticator) End(_ context.Context, id string) error {
	a.ended = append(a.ended, id)
	return nil
}

// TestMiddlewareConsultsTheAuthenticator pins the middleware's side of
// the boundary: it reads the credential a request presents, hands it to
// the injected authenticator under its form, and holds no verification
// of its own. A bearer token, when present, is the credential and the
// cookie is not consulted; logout ends the session the request presented.
func TestMiddlewareConsultsTheAuthenticator(t *testing.T) {
	a := &recordingAuthenticator{accept: map[authn.Form]string{authn.OperatorToken: "tok", authn.Session: "sess"}}
	ts := httptest.NewUnstartedServer(api.New(api.Deps{Authenticator: a}).Handler())
	ts.StartTLS()
	t.Cleanup(ts.Close)
	call := func(method, path, bearer, cookie string) int {
		t.Helper()
		req, err := http.NewRequest(method, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: api.SessionCookie, Value: cookie})
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	for _, tc := range []struct {
		name           string
		bearer, cookie string
		status         int
		want           authn.Credential
	}{
		{"token", "tok", "", http.StatusOK, authn.Credential{Form: authn.OperatorToken, Secret: "tok"}},
		{"session", "", "sess", http.StatusOK, authn.Credential{Form: authn.Session, Secret: "sess"}},
		{"token decides over a cookie", "bad", "sess", http.StatusUnauthorized, authn.Credential{Form: authn.OperatorToken, Secret: "bad"}},
		{"refused session", "", "stale", http.StatusUnauthorized, authn.Credential{Form: authn.Session, Secret: "stale"}},
	} {
		a.verified = nil
		if got := call(http.MethodGet, "/api/v1/me", tc.bearer, tc.cookie); got != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, got, tc.status)
		}
		if len(a.verified) != 1 || a.verified[0] != tc.want {
			t.Errorf("%s: verified %+v, want exactly %+v", tc.name, a.verified, tc.want)
		}
	}

	a.verified = nil
	if got := call(http.MethodGet, "/api/v1/me", "", ""); got != http.StatusUnauthorized || len(a.verified) != 0 {
		t.Errorf("no credential: status %d, verified %+v; want 401 with nothing verified", got, a.verified)
	}

	if got := call(http.MethodDelete, "/api/v1/session", "", "sess"); got != http.StatusOK {
		t.Fatalf("logout: status %d", got)
	}
	if len(a.ended) != 1 || a.ended[0] != "sess" {
		t.Fatalf("logout ended %v, want the presented session", a.ended)
	}
	a.ended = nil
	if got := call(http.MethodDelete, "/api/v1/session", "tok", ""); got != http.StatusOK || len(a.ended) != 0 {
		t.Fatalf("logout with a token: status %d, ended %v; want 200 with no session ended", got, a.ended)
	}
}
