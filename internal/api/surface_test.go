package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/authn"
	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/operator/operatortest"
)

// TestSurfaceComposition pins the composition the listener is built from:
// the middleware in its order, and the routes it lists are the routes the
// server reports.
func TestSurfaceComposition(t *testing.T) {
	srv := api.New(api.Deps{})
	sf := srv.Surface()
	var names []string
	for _, m := range sf.Middleware {
		names = append(names, m.Name)
	}
	if want := []string{"origin guard", "login throttle", "authentication"}; !slices.Equal(names, want) {
		t.Fatalf("middleware %v, want %v", names, want)
	}
	if !slices.Equal(sf.RouteNames(), srv.Routes()) {
		t.Fatalf("the composition's routes differ from the server's")
	}
	if sf.Console == nil {
		t.Fatal("no console handler composed")
	}
}

// TestPrincipalReachesTheBoundary pins principal propagation through the
// composition: a route registered on the surface, and a middleware
// inserted inside authentication, read the same principal from context
// whichever credential form authenticated the request, and an
// unauthenticated request reaches neither.
func TestPrincipalReachesTheBoundary(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc := &operator.Service{Store: operatortest.NewMemStore(), Now: func() time.Time { return now }}
	name := "Ada"
	if err := svc.SetPassword(ctx, testPassword, &name); err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.MintToken(ctx, "ci", 0)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := svc.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}

	sf := api.New(api.Deps{Authenticator: operator.NewAuthenticator(svc), Operators: svc, Now: func() time.Time { return now }}).Surface()
	var seenByMiddleware []string
	sf.Use(api.Middleware{Name: "principal probe", Wrap: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenByMiddleware = append(seenByMiddleware, principalName(r.Context()))
			next.ServeHTTP(w, r)
		})
	}})
	sf.Handle(http.MethodGet, api.APIPrefix+"/probe", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, principalName(r.Context()))
	})
	ts := httptest.NewUnstartedServer(sf.Handler())
	ts.StartTLS()
	t.Cleanup(ts.Close)

	probe := func(set func(*http.Request)) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+api.APIPrefix+"/probe", nil)
		if err != nil {
			t.Fatal(err)
		}
		set(req)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode, string(body)
	}
	if status, got := probe(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }); status != http.StatusOK || got != "Ada" {
		t.Fatalf("bearer: %d %q, want 200 naming Ada", status, got)
	}
	if status, got := probe(func(r *http.Request) { r.AddCookie(&http.Cookie{Name: api.SessionCookie, Value: session}) }); status != http.StatusOK || got != "Ada" {
		t.Fatalf("session: %d %q, want 200 naming Ada", status, got)
	}
	if status, _ := probe(func(*http.Request) {}); status != http.StatusUnauthorized {
		t.Fatalf("no credential: %d, want 401", status)
	}
	if want := []string{"Ada", "Ada"}; !slices.Equal(seenByMiddleware, want) {
		t.Fatalf("inserted middleware saw %v, want %v", seenByMiddleware, want)
	}
}

func principalName(ctx context.Context) string {
	p, ok := authn.FromContext(ctx)
	switch {
	case !ok:
		return "<none>"
	case p.DisplayName == nil:
		return "<unnamed>"
	default:
		return *p.DisplayName
	}
}
