package webutil

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
)

const csrfCookieName = "csrf_token"

// CSRFToken returns the CSRF token for this request, creating and setting a
// fresh cookie if one doesn't exist yet. Page() embeds this as "CSRFToken"
// in every page's data automatically; templates render it as a hidden field
// (or, for the htmx reassign dropdown, an hx-vals entry) in every
// state-changing form. RequireCSRF checks the two match on the way back in.
//
// The cookie isn't marked Secure: this app is also exercised directly over
// plain HTTP during local development/debugging (http://127.0.0.1:4455 —
// Oathkeeper doesn't terminate TLS there), and a Secure cookie silently
// wouldn't be stored on that path. HttpOnly + SameSite=Lax are the
// load-bearing protections here; Secure would be defense in depth on top,
// not the primary mechanism.
func CSRFToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	token := randomToken()
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return token
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// The system RNG failing means nothing on this machine is safe
		// anyway; there's no sane fallback.
		panic("webutil: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// RequireCSRF checks that state-changing requests (anything but
// GET/HEAD/OPTIONS) carry a csrf_token form value matching the csrf_token
// cookie — the standard double-submit pattern. An attacker's page can make
// a logged-in visitor's browser send a cross-site POST to this app, but it
// can't read the visitor's (HttpOnly) cookie to put the matching value in
// its form, so the forged request's token won't match.
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "Missing CSRF token — please reload the page and try again.", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Could not parse form.", http.StatusBadRequest)
			return
		}
		if r.PostFormValue(csrfCookieName) != cookie.Value {
			http.Error(w, "Invalid CSRF token — please reload the page and try again.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
