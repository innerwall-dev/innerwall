package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/innerwall-dev/innerwall/internal/api"
)

// consoleTree is a built console as Vite lays it out: an entry point, a
// content-addressed script under assets/, and one plain public file.
func consoleTree() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                    {Data: []byte("<!doctype html><title>Innerwall</title><script type=module src=/assets/index-abc123.js></script>")},
		"assets/index-abc123.js":        {Data: []byte("console.log('innerwall')")},
		"assets/plex-sans-9f8e7d.woff2": {Data: []byte("woff2")},
		"favicon.svg":                   {Data: []byte("<svg/>")},
	}
}

// get fetches a console path and returns the reply with its body as text.
func (s *surface) get(t *testing.T, p string) (*reply, string) {
	t.Helper()
	resp, err := s.client(false).Get(s.ts.URL + p)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return &reply{method: http.MethodGet, path: p, status: resp.StatusCode, header: resp.Header}, string(raw)
}

func TestConsoleAssetsAreServedImmutable(t *testing.T) {
	s := newSurface(t, api.Deps{Console: consoleTree()})
	for _, p := range []string{"/assets/index-abc123.js", "/assets/plex-sans-9f8e7d.woff2"} {
		resp, body := s.get(t, p)
		if resp.status != http.StatusOK {
			t.Fatalf("GET %s: status %d", p, resp.status)
		}
		if got := resp.header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Fatalf("GET %s: Cache-Control %q, want immutable", p, got)
		}
		if body == "" || body != string(consoleTree()[strings.TrimPrefix(p, "/")].Data) {
			t.Fatalf("GET %s: body %q", p, body)
		}
		if resp.header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("GET %s: missing nosniff", p)
		}
	}
	resp, _ := s.get(t, "/assets/index-abc123.js")
	if ct := resp.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("script content type %q", ct)
	}
	resp, _ = s.get(t, "/assets/plex-sans-9f8e7d.woff2")
	if ct := resp.header.Get("Content-Type"); ct != "font/woff2" {
		t.Fatalf("font content type %q", ct)
	}
}

func TestConsoleFallsBackToEntryPoint(t *testing.T) {
	s := newSurface(t, api.Deps{Console: consoleTree()})
	entry := string(consoleTree()["index.html"].Data)
	for _, p := range []string{"/", "/login", "/workloads/2f0c1f0e", "/policy/", "/no/such/route?x=1", "/index.html"} {
		resp, body := s.get(t, p)
		if resp.status != http.StatusOK {
			t.Fatalf("GET %s: status %d", p, resp.status)
		}
		if body != entry {
			t.Fatalf("GET %s: body %q, want the entry point", p, body)
		}
		if got := resp.header.Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("GET %s: Cache-Control %q, want no-cache", p, got)
		}
		if resp.header.Get("ETag") == "" {
			t.Fatalf("GET %s: no ETag on the entry point", p)
		}
		if ct := resp.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("GET %s: content type %q", p, ct)
		}
	}
	// A plain public file is itself, with revalidation.
	resp, body := s.get(t, "/favicon.svg")
	if resp.status != http.StatusOK || body != "<svg/>" || resp.header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("GET /favicon.svg: status %d body %q cache %q", resp.status, body, resp.header.Get("Cache-Control"))
	}
}

func TestConsoleEntryPointRevalidates(t *testing.T) {
	s := newSurface(t, api.Deps{Console: consoleTree()})
	resp, _ := s.get(t, "/login")
	again, _ := s.do(t, s.client(false), request{method: http.MethodGet, path: "/login", headers: map[string]string{"If-None-Match": resp.header.Get("ETag")}})
	if again.status != http.StatusNotModified {
		t.Fatalf("revalidation: status %d, want 304", again.status)
	}
}

func TestConsoleMissingAssetIsNotTheEntryPoint(t *testing.T) {
	s := newSurface(t, api.Deps{Console: consoleTree()})
	resp, body := s.do(t, s.client(false), request{method: http.MethodGet, path: "/assets/index-stale00.js"})
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
}

func TestConsoleServesOnlyReads(t *testing.T) {
	s := newSurface(t, api.Deps{Console: consoleTree()})
	resp, body := s.do(t, s.client(false), request{method: http.MethodPost, path: "/login", body: `{}`})
	expectProblem(t, resp, body, http.StatusMethodNotAllowed, api.ProblemMethodNotAllowed)
	if resp.header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("Allow %q", resp.header.Get("Allow"))
	}
}

// The API prefix is the surface's, whatever the console holds: a path
// under it that no handler claims is the surface's not-found, never the
// entry point.
func TestConsoleNeverShadowsTheAPIPrefix(t *testing.T) {
	s := newSurface(t, api.Deps{Console: consoleTree()})
	resp, body := s.do(t, s.client(false), request{method: http.MethodGet, path: "/api/v1/no-such-endpoint"})
	expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemUnauthenticated)
	s.setPassword(t, "")
	c := s.client(true)
	resp, body = s.do(t, c, login(testPassword))
	if resp.status != http.StatusOK {
		t.Fatalf("login: %d %v", resp.status, body)
	}
	resp, body = s.do(t, c, request{method: http.MethodGet, path: "/api/v1/no-such-endpoint"})
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
}

// A build with no console, and a checkout whose tree holds no entry point
// yet, both answer from the marked mount point.
func TestConsoleAbsentIsTheMountPoint(t *testing.T) {
	for name, tree := range map[string]fstest.MapFS{"nil": nil, "placeholder": {".gitkeep": {Data: nil}}} {
		t.Run(name, func(t *testing.T) {
			deps := api.Deps{}
			if tree != nil {
				deps.Console = tree
			}
			s := newSurface(t, deps)
			for _, p := range []string{"/", "/login", "/assets/index-abc123.js"} {
				resp, body := s.do(t, s.client(false), request{method: http.MethodGet, path: p})
				expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
				if !strings.Contains(body["detail"].(string), "not served by this build") {
					t.Fatalf("GET %s: detail %v", p, body["detail"])
				}
			}
		})
	}
}
