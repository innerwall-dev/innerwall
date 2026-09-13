package api

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Login throttling defaults: attempts per source address per fixed window.
const (
	DefaultLoginAttempts = 10
	DefaultLoginWindow   = time.Minute
)

// throttleMaxSources bounds the in-process table; when it is exceeded,
// windows that have already elapsed are dropped.
const throttleMaxSources = 4096

// Throttle is a fixed-window attempt limiter keyed by source address. It
// lives in one process and protects that process: a brake on online
// password guessing, not a distributed rate limiter (ADR-0021). The key is
// the connection's own address, never a forwarded header, so a proxy in
// front collapses every client into one source and the brake tightens
// accordingly.
type Throttle struct {
	// Limit is the number of attempts allowed per window;
	// DefaultLoginAttempts if zero.
	Limit int
	// Window is the fixed window length; DefaultLoginWindow if zero.
	Window time.Duration
	// Now is the clock; time.Now if nil.
	Now func() time.Time

	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	start time.Time
	count int
}

func (t *Throttle) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *Throttle) limit() int {
	if t.Limit > 0 {
		return t.Limit
	}
	return DefaultLoginAttempts
}

func (t *Throttle) window() time.Duration {
	if t.Window > 0 {
		return t.Window
	}
	return DefaultLoginWindow
}

// Allow records one attempt from key and reports whether it is within the
// window's budget; when it is not, retryAfter is the time until the window
// resets.
func (t *Throttle) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.windows == nil {
		t.windows = map[string]*window{}
	}
	w := t.windows[key]
	if w == nil || now.Sub(w.start) >= t.window() {
		if len(t.windows) >= throttleMaxSources {
			t.evict(now)
		}
		w = &window{start: now}
		t.windows[key] = w
	}
	w.count++
	if w.count > t.limit() {
		return false, w.start.Add(t.window()).Sub(now)
	}
	return true, 0
}

// evict drops every elapsed window. Called with the lock held.
func (t *Throttle) evict(now time.Time) {
	for k, w := range t.windows {
		if now.Sub(w.start) >= t.window() {
			delete(t.windows, k)
		}
	}
}

// Middleware applies the throttle to the login route only.
func (t *Throttle) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != APIPrefix+"/session" {
			next.ServeHTTP(w, r)
			return
		}
		if ok, retryAfter := t.Allow(sourceAddress(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter/time.Second)+1))
			writeProblem(w, Problem{Type: ProblemTooManyAttempts, Title: "Too many login attempts", Status: http.StatusTooManyRequests, Detail: "wait before trying again"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sourceAddress is the connection's host part, without the port, so a
// client's reconnects share one window.
func sourceAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
