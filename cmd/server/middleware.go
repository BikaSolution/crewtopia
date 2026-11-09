package main

import (
	"net/http"

	"crewtopia/internal/authctx"
)

// securityHeaders sets the baseline hardening headers Go doesn't send by
// default. CSP is deliberately strict on script-src (no 'unsafe-inline') —
// every inline onclick/onsubmit handler was moved to web/static/app.js
// specifically so this could be tight; style-src stays relaxed since a
// handful of templates use inline style="" for one-off layout tweaks, and
// CSS injection is a much smaller blast radius than script injection.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"font-src 'self'; " +
		"connect-src 'self'; " +
		"form-action 'self'; " +
		"base-uri 'self'; " +
		"frame-ancestors 'none'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// requireTrustedHeaders is the Phase 7 replacement for Phase 6's direct
// Kratos whoami call: Oathkeeper has already authenticated the session and
// injected X-User-* headers by the time a request reaches here, so the app
// just reads them.
//
// Trusting headers this way is only sound if the app is unreachable except
// through Oathkeeper — true since Phase 9's containerization: the app has no
// published port and isn't reachable from the host or the internet, only
// from other containers on the compose network (Oathkeeper, specifically).
func requireTrustedHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-User-Id")
		if id == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		u := authctx.User{
			ID:    id,
			Email: r.Header.Get("X-User-Email"),
			Role:  r.Header.Get("X-User-Role"),
		}
		next.ServeHTTP(w, r.WithContext(authctx.WithUser(r.Context(), u)))
	})
}
