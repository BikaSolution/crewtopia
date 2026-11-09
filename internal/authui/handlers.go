// Package authui renders the login/recovery/settings pages against Ory
// Kratos's self-service flows. Kratos doesn't ship its own HTML — it returns
// a JSON description of the form (action URL, fields, error messages) and
// expects the application to render it; these handlers do that rendering
// generically enough to cover all three flow types with one template.
package authui

import (
	"encoding/json"
	"net/http"

	"crewtopia/internal/kratos"
	"crewtopia/internal/webutil"
)

type Handlers struct {
	kratos *kratos.Client
}

func NewHandlers(client *kratos.Client) *Handlers {
	return &Handlers{kratos: client}
}

func (h *Handlers) renderFlow(w http.ResponseWriter, r *http.Request, kind, title string) {
	flowID := r.URL.Query().Get("flow")
	flow, err := h.kratos.GetFlow(r.Context(), kind, flowID, r.Header.Get("Cookie"))
	if err != nil {
		webutil.UpstreamError(w, r, err, "get "+kind+" flow")
		return
	}
	webutil.Page(w, r, "auth_flow.html", map[string]any{
		"ActiveNav": "",
		"Title":     title,
		"Flow":      flow,
	})
}

// Login shows the sign-in form. With no ?flow= yet, it kicks off a new
// login flow by sending the browser to Kratos (through our same-origin
// proxy, so the flow's CSRF/session cookies land on our own domain), which
// redirects back here with one.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("flow") == "" {
		http.Redirect(w, r, kratos.PublicProxyPrefix+"/self-service/login/browser", http.StatusSeeOther)
		return
	}
	h.renderFlow(w, r, "login", "Sign in")
}

// Logout asks Kratos for a one-time logout URL and sends the browser there;
// visiting it (through the proxy) clears the session cookie and redirects
// to the configured post-logout page.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.kratos.PublicURL+"/self-service/logout/browser", nil)
	if err != nil {
		webutil.ServerError(w, r, err, "build logout request")
		return
	}
	req.Header.Set("Cookie", r.Header.Get("Cookie"))
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		webutil.UpstreamError(w, r, err, "fetch logout url")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// No active session to log out of.
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	var body struct {
		LogoutURL string `json:"logout_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		webutil.UpstreamError(w, r, err, "decode logout response")
		return
	}
	http.Redirect(w, r, body.LogoutURL, http.StatusSeeOther)
}

// Recovery renders the account-recovery form. Reached only via the one-time
// link the super-admin invite flow generates (Phase 8) — there's no public
// "forgot password" entry point since self-service registration is disabled
// entirely.
func (h *Handlers) Recovery(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("flow") == "" {
		http.Error(w, "missing recovery flow — use the link from your invite", http.StatusBadRequest)
		return
	}
	h.renderFlow(w, r, "recovery", "Set your password")
}

// Settings renders the account-settings form (password change). Reached
// after a successful recovery, or by a logged-in user visiting directly.
func (h *Handlers) Settings(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("flow") == "" {
		http.Redirect(w, r, kratos.PublicProxyPrefix+"/self-service/settings/browser", http.StatusSeeOther)
		return
	}
	h.renderFlow(w, r, "settings", "Account settings")
}
