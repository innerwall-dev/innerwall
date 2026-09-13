package api

import (
	"net/http"
	"net/url"
	"strings"
)

// CrossOriginGuard refuses every request that is not a read and that a
// browser marks as coming from another origin. The console is served from
// the same origin as the façade and the session cookie does not travel on
// cross-site navigations, so this check is the whole cross-site request
// forgery defense and no token scheme is needed (ADR-0021).
//
// Two signals are consulted. A browser that sends Sec-Fetch-Site says
// outright whether the request is same-origin; anything else is refused.
// A browser that sends only Origin is held to the origin this listener is
// serving: the request's own host, over TLS. A client that sends neither
// header is not a browser and is not subject to forgery through ambient
// credentials, so it passes.
func CrossOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reason, ok := crossOrigin(r); !ok {
			writeProblem(w, Problem{Type: ProblemCrossOrigin, Title: "Cross-origin request refused", Status: http.StatusForbidden, Detail: reason})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// crossOrigin reports whether the request may proceed, and why not.
func crossOrigin(r *http.Request) (reason string, ok bool) {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return "", true
	}
	switch site := r.Header.Get("Sec-Fetch-Site"); site {
	case "", "same-origin", "none":
	default:
		return "request was made from a " + site + " site", false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return "", true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, r.Host) {
		return "request origin does not match this listener", false
	}
	return "", true
}
