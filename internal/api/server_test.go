package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/operator/operatortest"
)

const testPassword = "a perfectly fine password"

type surface struct {
	ts        *httptest.Server
	operators *operator.Service
	store     *operatortest.MemStore
	now       *time.Time
}

func newSurface(t *testing.T, deps api.Deps) *surface {
	t.Helper()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	st := operatortest.NewMemStore()
	svc := &operator.Service{Store: st, Now: func() time.Time { return now }}
	deps.Operators = svc
	deps.Now = func() time.Time { return now }
	ts := httptest.NewUnstartedServer(api.New(deps).Handler())
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return &surface{ts: ts, operators: svc, store: st, now: &now}
}

// client returns an HTTPS client that trusts the test listener and, when
// jar is set, keeps cookies like a browser.
func (s *surface) client(jar bool) *http.Client {
	c := s.ts.Client()
	if jar {
		c.Jar, _ = cookiejar.New(nil)
	}
	return c
}

func (s *surface) setPassword(t *testing.T, name string) {
	t.Helper()
	var display *string
	if name != "" {
		display = &name
	}
	if err := s.operators.SetPassword(context.Background(), testPassword, display); err != nil {
		t.Fatal(err)
	}
}

type request struct {
	method, path, body string
	headers            map[string]string
}

// reply is what a request came back with; the body has been read and closed.
type reply struct {
	method, path string
	status       int
	header       http.Header
	cookies      []*http.Cookie
}

func (s *surface) do(t *testing.T, c *http.Client, r request) (*reply, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(r.method, s.ts.URL+r.path, strings.NewReader(r.body))
	if err != nil {
		t.Fatal(err)
	}
	if r.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("%s %s: body is not JSON: %q", r.method, r.path, raw)
		}
	}
	return &reply{method: r.method, path: r.path, status: resp.StatusCode, header: resp.Header, cookies: resp.Cookies()}, body
}

func login(password string) request {
	return request{method: http.MethodPost, path: "/api/v1/session", body: `{"password":` + strconvQuote(password) + `}`}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func expectProblem(t *testing.T, resp *reply, body map[string]any, status int, typ string) {
	t.Helper()
	if resp.status != status {
		t.Fatalf("%s %s: status %d, want %d (body %v)", resp.method, resp.path, resp.status, status, body)
	}
	if ct := resp.header.Get("Content-Type"); ct != api.ContentTypeProblem {
		t.Fatalf("content type %q, want problem document", ct)
	}
	if body["type"] != typ || body["status"] != float64(status) || body["title"] == "" {
		t.Fatalf("problem %v, want type %s status %d", body, typ, status)
	}
}

func TestLoginLogoutFlow(t *testing.T) {
	s := newSurface(t, api.Deps{Site: "lab"})
	browser := s.client(true)

	// Fresh install: no password yet is its own problem type, and the
	// endpoint offers no way to set one.
	resp, body := s.do(t, browser, login(testPassword))
	expectProblem(t, resp, body, http.StatusForbidden, api.ProblemNoPassword)
	if !strings.Contains(body["detail"].(string), "set-password") {
		t.Fatalf("fresh-install detail does not name the command: %v", body)
	}

	s.setPassword(t, "Ada")
	resp, body = s.do(t, browser, login("a perfectly wrong password"))
	expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemInvalidCredentials)
	if resp.header.Get("Set-Cookie") != "" {
		t.Fatal("a failed login set a cookie")
	}

	resp, body = s.do(t, browser, login(testPassword))
	if resp.status != http.StatusOK {
		t.Fatalf("login: %d %v", resp.status, body)
	}
	if body["display_name"] != "Ada" || body["site"] != "lab" {
		t.Fatalf("me object %v", body)
	}
	if ct := resp.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type %q", ct)
	}
	cookies := resp.cookies
	if len(cookies) != 1 {
		t.Fatalf("login set %d cookies, want 1: %v", len(cookies), resp.header["Set-Cookie"])
	}
	c := cookies[0]
	if c.Name != api.SessionCookie || c.Value == "" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != int(operator.DefaultSessionTTL/time.Second) {
		t.Fatalf("session cookie attributes: %s", resp.header.Get("Set-Cookie"))
	}
	// The cookie authenticates; the browser sends it on its own.
	resp, body = s.do(t, browser, request{method: http.MethodGet, path: "/api/v1/me"})
	if resp.status != http.StatusOK || body["display_name"] != "Ada" || body["site"] != "lab" {
		t.Fatalf("me: %d %v", resp.status, body)
	}

	// Logout revokes the session and clears the cookie.
	resp, body = s.do(t, browser, request{method: http.MethodDelete, path: "/api/v1/session"})
	if resp.status != http.StatusOK {
		t.Fatalf("logout: %d %v", resp.status, body)
	}
	cleared := resp.cookies
	if len(cleared) != 1 || cleared[0].Name != api.SessionCookie || cleared[0].Value != "" || cleared[0].MaxAge != -1 || !cleared[0].HttpOnly || !cleared[0].Secure {
		t.Fatalf("logout cookie: %s", resp.header.Get("Set-Cookie"))
	}
	if s.store.SessionCount() != 0 {
		t.Fatalf("%d sessions remain after logout", s.store.SessionCount())
	}
	resp, body = s.do(t, browser, request{method: http.MethodGet, path: "/api/v1/me"})
	expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemUnauthenticated)
	if resp.header.Get("WWW-Authenticate") == "" {
		t.Fatal("401 without a WWW-Authenticate challenge")
	}
}

func TestMeWithoutDisplayName(t *testing.T) {
	s := newSurface(t, api.Deps{})
	s.setPassword(t, "")
	resp, body := s.do(t, s.client(true), login(testPassword))
	if resp.status != http.StatusOK {
		t.Fatalf("login: %d %v", resp.status, body)
	}
	if v, present := body["display_name"]; !present || v != nil {
		t.Fatalf("display_name should be null: %v", body)
	}
	if v, present := body["site"]; !present || v != "" {
		t.Fatalf("site should be empty: %v", body)
	}
	// Unconfigured, the advertised gateway address is present and null;
	// nothing is derived in its place.
	if v, present := body["gateway_address"]; !present || v != nil {
		t.Fatalf("gateway_address should be null: %v", body)
	}
}

func TestMeCarriesAdvertisedGatewayAddress(t *testing.T) {
	s := newSurface(t, api.Deps{Site: "lab", GatewayAddress: "gateway.lab.example:8443"})
	s.setPassword(t, "Ada")
	browser := s.client(true)
	resp, body := s.do(t, browser, login(testPassword))
	if resp.status != http.StatusOK || body["gateway_address"] != "gateway.lab.example:8443" {
		t.Fatalf("login: %d %v", resp.status, body)
	}
	resp, body = s.do(t, browser, request{method: http.MethodGet, path: "/api/v1/me"})
	if resp.status != http.StatusOK || body["gateway_address"] != "gateway.lab.example:8443" || body["site"] != "lab" {
		t.Fatalf("me: %d %v", resp.status, body)
	}
}

func TestMiddlewareMatrix(t *testing.T) {
	s := newSurface(t, api.Deps{})
	s.setPassword(t, "Ada")
	ctx := context.Background()

	expiredSession, _, err := s.operators.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Advance past that session's lifetime, then create the live one.
	*s.now = s.now.Add(operator.DefaultSessionTTL)
	liveSession, _, err := s.operators.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	liveToken, _, err := s.operators.MintToken(ctx, "live", 0)
	if err != nil {
		t.Fatal(err)
	}
	revokedToken, revoked, err := s.operators.MintToken(ctx, "revoked", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.operators.RevokeToken(ctx, revoked.ID); err != nil {
		t.Fatal(err)
	}
	expiredToken, _, err := s.operators.MintToken(ctx, "expired", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	*s.now = s.now.Add(2 * time.Minute)

	cases := []struct {
		name    string
		headers map[string]string
		status  int
	}{
		{"no credential", nil, http.StatusUnauthorized},
		{"malformed authorization", map[string]string{"Authorization": "Bearer"}, http.StatusUnauthorized},
		{"wrong scheme", map[string]string{"Authorization": "Basic " + liveToken}, http.StatusUnauthorized},
		{"malformed token", map[string]string{"Authorization": "Bearer iwo_not-a-token"}, http.StatusUnauthorized},
		{"provisioning token", map[string]string{"Authorization": "Bearer iw_" + liveToken[4:]}, http.StatusUnauthorized},
		{"unknown token", map[string]string{"Authorization": "Bearer " + operator.TokenPrefix + strings.Repeat("A", 43)}, http.StatusUnauthorized},
		{"revoked token", map[string]string{"Authorization": "Bearer " + revokedToken}, http.StatusUnauthorized},
		{"expired token", map[string]string{"Authorization": "Bearer " + expiredToken}, http.StatusUnauthorized},
		{"malformed cookie", map[string]string{"Cookie": api.SessionCookie + "=garbage"}, http.StatusUnauthorized},
		{"unknown session", map[string]string{"Cookie": api.SessionCookie + "=" + strings.Repeat("A", 43)}, http.StatusUnauthorized},
		{"expired session", map[string]string{"Cookie": api.SessionCookie + "=" + expiredSession}, http.StatusUnauthorized},
		{"valid cookie", map[string]string{"Cookie": api.SessionCookie + "=" + liveSession}, http.StatusOK},
		{"valid bearer", map[string]string{"Authorization": "Bearer " + liveToken}, http.StatusOK},
		{"bearer case-insensitive", map[string]string{"Authorization": "bearer " + liveToken}, http.StatusOK},
		{"bearer outranks a dead cookie", map[string]string{"Authorization": "Bearer " + liveToken, "Cookie": api.SessionCookie + "=" + expiredSession}, http.StatusOK},
		{"dead bearer is not rescued by a live cookie", map[string]string{"Authorization": "Bearer " + revokedToken, "Cookie": api.SessionCookie + "=" + liveSession}, http.StatusUnauthorized},
	}
	c := s.client(false)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := s.do(t, c, request{method: http.MethodGet, path: "/api/v1/me", headers: tc.headers})
			if tc.status == http.StatusOK {
				if resp.status != http.StatusOK || body["display_name"] != "Ada" {
					t.Fatalf("%d %v", resp.status, body)
				}
				return
			}
			expectProblem(t, resp, body, tc.status, api.ProblemUnauthenticated)
			// An invalid cookie is cleared; a missing one is not touched.
			_, hadCookie := tc.headers["Cookie"]
			if cleared := resp.cookies; hadCookie != (len(cleared) == 1 && cleared[0].MaxAge == -1) {
				t.Fatalf("cookie clearing: had=%v set-cookie=%q", hadCookie, resp.header.Get("Set-Cookie"))
			}
		})
	}

	// A token's use is recorded on the path; the revoked and expired ones
	// were never used.
	tokens, err := s.operators.ListTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		used := tok.LastUsedAt != nil
		if want := tok.Name == "live"; used != want {
			t.Errorf("token %s used=%v, want %v", tok.Name, used, want)
		}
	}
}

func TestOriginGuard(t *testing.T) {
	s := newSurface(t, api.Deps{})
	s.setPassword(t, "Ada")
	c := s.client(false)
	sameOrigin := "https://" + strings.TrimPrefix(s.ts.URL, "https://")

	cases := []struct {
		name    string
		method  string
		headers map[string]string
		cross   bool
	}{
		{"no browser headers", http.MethodPost, nil, false},
		{"same-origin fetch", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": sameOrigin}, false},
		{"origin only, same host", http.MethodPost, map[string]string{"Origin": sameOrigin}, false},
		{"cross-site fetch", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://elsewhere.example"}, true},
		{"same-site fetch", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-site"}, true},
		{"foreign origin", http.MethodPost, map[string]string{"Origin": "https://elsewhere.example"}, true},
		{"plaintext origin", http.MethodPost, map[string]string{"Origin": "http://" + strings.TrimPrefix(s.ts.URL, "https://")}, true},
		{"null origin", http.MethodPost, map[string]string{"Origin": "null"}, true},
		{"cross-site delete", http.MethodDelete, map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{"cross-site read is not the guard's concern", http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://elsewhere.example"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := request{method: tc.method, path: "/api/v1/session", headers: tc.headers}
			if tc.method == http.MethodGet {
				r.path = "/api/v1/me"
			}
			resp, body := s.do(t, c, r)
			if tc.cross {
				expectProblem(t, resp, body, http.StatusForbidden, api.ProblemCrossOrigin)
				return
			}
			if body["type"] == api.ProblemCrossOrigin {
				t.Fatalf("same-origin request refused: %v", body)
			}
		})
	}
}

func TestLoginThrottle(t *testing.T) {
	s := newSurface(t, api.Deps{LoginAttempts: 3, LoginWindow: time.Minute})
	s.setPassword(t, "Ada")
	c := s.client(false)

	for i := range 3 {
		resp, body := s.do(t, c, login("wrong password, attempt "+string(rune('0'+i))))
		expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemInvalidCredentials)
	}
	resp, body := s.do(t, c, login(testPassword))
	expectProblem(t, resp, body, http.StatusTooManyRequests, api.ProblemTooManyAttempts)
	if resp.header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	// Reads are not throttled.
	resp, body = s.do(t, c, request{method: http.MethodGet, path: "/api/v1/me"})
	expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemUnauthenticated)
	// The window is fixed: it resets when it elapses, not per attempt.
	*s.now = s.now.Add(59 * time.Second)
	resp, body = s.do(t, c, login(testPassword))
	expectProblem(t, resp, body, http.StatusTooManyRequests, api.ProblemTooManyAttempts)
	*s.now = s.now.Add(time.Second)
	resp, body = s.do(t, c, login(testPassword))
	if resp.status != http.StatusOK {
		t.Fatalf("after the window: %d %v", resp.status, body)
	}
}

func TestRoutingAndMountPoint(t *testing.T) {
	s := newSurface(t, api.Deps{})
	c := s.client(false)

	resp, body := s.do(t, c, request{method: http.MethodGet, path: "/"})
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, body = s.do(t, c, request{method: http.MethodGet, path: "/console/anything"})
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	// Unknown API paths are refused before authentication is even
	// consulted, and are still problem documents.
	resp, body = s.do(t, c, request{method: http.MethodGet, path: "/api/v1/nothing"})
	if resp.status != http.StatusUnauthorized && resp.status != http.StatusNotFound {
		t.Fatalf("unknown path: %d %v", resp.status, body)
	}
	if resp.header.Get("Content-Type") != api.ContentTypeProblem {
		t.Fatalf("unknown path content type %q", resp.header.Get("Content-Type"))
	}
	// A malformed body on the one public route is an invalid request, as
	// is a body holding more than one document.
	resp, body = s.do(t, c, request{method: http.MethodPost, path: "/api/v1/session", body: `{"password":`})
	expectProblem(t, resp, body, http.StatusBadRequest, api.ProblemInvalidRequest)
	resp, body = s.do(t, c, request{method: http.MethodPost, path: "/api/v1/session", body: `{"password":"x"} {"password":"y"}`})
	expectProblem(t, resp, body, http.StatusBadRequest, api.ProblemInvalidRequest)

	// A known path with an unsupported method is 405 with Allow, once
	// authenticated; an unknown path under the prefix is 404.
	s.setPassword(t, "Ada")
	token, _, err := s.operators.MintToken(context.Background(), "t", 0)
	if err != nil {
		t.Fatal(err)
	}
	bearer := map[string]string{"Authorization": "Bearer " + token}
	resp, body = s.do(t, c, request{method: http.MethodPut, path: "/api/v1/me", headers: bearer})
	expectProblem(t, resp, body, http.StatusMethodNotAllowed, api.ProblemMethodNotAllowed)
	if resp.header.Get("Allow") != "GET" {
		t.Fatalf("Allow %q", resp.header.Get("Allow"))
	}
	resp, body = s.do(t, c, request{method: http.MethodGet, path: "/api/v1/nothing", headers: bearer})
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
}
