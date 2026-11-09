// Package webutil renders the server-side HTML templates shared by every
// page/partial handler.
package webutil

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"crewtopia/internal/authctx"
	"crewtopia/internal/weekday"
)

// TemplatesDir is the base directory templates are read from. Templates are
// re-parsed on every render (no caching), which keeps edits visible without
// restarting the server — an acceptable cost at this app's scale.
var TemplatesDir = "web/templates"

// assetVersion is appended as a "?v=" query string to static asset links
// (styles.css, htmx.min.js) so that a redeploy busts any cache sitting in
// front of the app — Cloudflare's edge cache in particular will happily keep
// serving a stale styles.css for a long time otherwise, since the URL never
// changes on its own.
var assetVersion = fmt.Sprint(time.Now().Unix())

var funcMap = template.FuncMap{
	"assetVersion": func() string { return assetVersion },
	"hasDay":       func(mask weekday.Mask, day weekday.Mask) bool { return mask.Has(day) },
	"weekdays": func() []struct {
		Bit   weekday.Mask
		Label string
		Short string
	} {
		return weekday.Ordered
	},
	"desirePct": func(v float64) string { return fmt.Sprintf("%.2f%%", v) },
	"contains": func(haystack []int64, needle int64) bool {
		for _, v := range haystack {
			if v == needle {
				return true
			}
		}
		return false
	},
	"currentYear": func() int { return time.Now().Year() },
}

// ServerError logs the real error server-side (with request context) and
// sends the client a generic message instead. Never send err.Error() to the
// client directly — it can embed things a visitor has no business seeing:
// DB error text, file paths, upstream service URLs/response bodies.
func ServerError(w http.ResponseWriter, r *http.Request, err error, context string) {
	log.Printf("500 %s %s: %s: %v", r.Method, r.URL.Path, context, err)
	http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
}

// UpstreamError is ServerError for failures talking to Kratos — same
// reasoning, its error bodies can include internal URLs like
// "http://kratos:4433/...".
func UpstreamError(w http.ResponseWriter, r *http.Request, err error, context string) {
	log.Printf("502 %s %s: %s: %v", r.Method, r.URL.Path, context, err)
	http.Error(w, "The identity service isn't responding. Please try again.", http.StatusBadGateway)
}

// Page renders web/templates/layout.html with the named page file layered in
// (the page file's {{define "content"}} block fills the layout's content
// slot). It injects "CurrentUser" from the request's context (set by the
// session middleware) so layout.html can show who's logged in without every
// handler having to pass it explicitly.
func Page(w http.ResponseWriter, r *http.Request, pageFile string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	// Always present (even zero-valued) so templates can write
	// {{.CurrentUser.Email}} without erroring on the public/logged-out pages.
	user, _ := authctx.FromContext(r.Context())
	data["CurrentUser"] = user
	// Only fill this in if the handler didn't already (Reassign/Edit compute
	// it early to stamp onto per-cell data structures before Page ever runs).
	// CSRFToken sets a fresh cookie on the response the first time it's
	// called for a request with none yet, so calling it a second time here
	// with a *different* result would desync the cookie from a token a
	// handler already burned into rendered HTML earlier in the same request.
	if _, ok := data["CSRFToken"]; !ok {
		data["CSRFToken"] = CSRFToken(w, r)
	}

	files := []string{
		filepath.Join(TemplatesDir, "layout.html"),
		filepath.Join(TemplatesDir, pageFile),
	}
	// Partials are always included so any page can {{template "name" .}} one
	// of them (e.g. the editable roster page reusing the "roster_cell"
	// partial that the htmx reassign endpoint also renders standalone).
	if partials, err := filepath.Glob(filepath.Join(TemplatesDir, "partials", "*.html")); err == nil {
		files = append(files, partials...)
	}

	tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(files...)
	if err != nil {
		ServerError(w, r, err, "parse templates")
		return
	}
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		ServerError(w, r, err, "execute template "+pageFile)
	}
}

// Partial renders a single named template from web/templates/partials, for
// htmx swaps that replace a fragment of the page rather than the whole thing.
func Partial(w http.ResponseWriter, r *http.Request, partialFile, name string, data any) {
	tmpl, err := template.New(name).Funcs(funcMap).ParseFiles(
		filepath.Join(TemplatesDir, "partials", partialFile),
	)
	if err != nil {
		ServerError(w, r, err, "parse partial "+partialFile)
		return
	}
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		ServerError(w, r, err, "execute partial "+name)
	}
}
