package api

import (
	"container/list"
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

// throttleMaxSources bounds the in-process table. A new source at the
// bound first drops windows that have elapsed and then, if the table is
// still full, the window that started earliest, even though it has not
// elapsed: the table never holds more than this many sources, and each
// attempt costs constant work, however many distinct sources arrive.
const throttleMaxSources = 4096

// Throttle is a fixed-window attempt limiter keyed by source address. It
// lives in one process and protects that process: a brake on online
// password guessing, not a distributed rate limiter (ADR-0021). The key is
// the connection's own address, never a forwarded header, so a proxy in
// front collapses every client into one source and the brake tightens
// accordingly. The table is bounded (throttleMaxSources), so the brake
// is bounded too: a guesser that rotates through more source addresses
// than the bound within one window pushes its own oldest windows out and
// resets their counts. Against that, the throttle costs the process
// bounded memory and constant work per attempt, and nothing more is
// claimed; protection from a distributed guesser sits in front of the
// control plane (ADR-0021).
type Throttle struct {
	// Limit is the number of attempts allowed per window;
	// DefaultLoginAttempts if zero.
	Limit int
	// Window is the fixed window length; DefaultLoginWindow if zero.
	Window time.Duration
	// Now is the clock; time.Now if nil.
	Now func() time.Time

	mu      sync.Mutex
	windows map[string]*list.Element
	// order holds every window, earliest start first: a window is
	// appended when it starts, so the front is always the oldest.
	order *list.List
}

type window struct {
	key   string
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
		t.windows = map[string]*list.Element{}
		t.order = list.New()
	}
	var w *window
	if e, ok := t.windows[key]; ok {
		w = e.Value.(*window) //nolint:forcetypeassert // the list holds only windows
		if now.Sub(w.start) >= t.window() {
			// A new window for a known source starts now: it is the
			// newest, so it moves to the back.
			w.start, w.count = now, 0
			t.order.MoveToBack(e)
		}
	} else {
		t.makeRoom()
		w = &window{key: key, start: now}
		t.windows[key] = t.order.PushBack(w)
	}
	w.count++
	if w.count > t.limit() {
		return false, w.start.Add(t.window()).Sub(now)
	}
	return true, 0
}

// makeRoom makes space for one more source by dropping the oldest window
// while the table is full. Windows are ordered by start, so the elapsed
// ones go first; a window still running goes only when every older one
// has gone and the table is still full. Called with the lock held.
func (t *Throttle) makeRoom() {
	for len(t.windows) >= throttleMaxSources {
		front := t.order.Front()
		w := front.Value.(*window) //nolint:forcetypeassert // the list holds only windows
		t.order.Remove(front)
		delete(t.windows, w.key)
	}
}

// Sources returns the number of sources the table holds.
func (t *Throttle) Sources() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.windows)
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
