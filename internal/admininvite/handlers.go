// Package admininvite lets a super admin add a new login — the only way a
// Crewtopia account gets created, since self-service registration is
// disabled entirely (see deploy/kratos/kratos.yml).
package admininvite

import (
	"net/http"

	"crewtopia/internal/authctx"
	"crewtopia/internal/kratos"
	"crewtopia/internal/webutil"
)

type Handlers struct {
	kratos *kratos.Client
}

func NewHandlers(client *kratos.Client) *Handlers {
	return &Handlers{kratos: client}
}

func (h *Handlers) NewForm(w http.ResponseWriter, r *http.Request) {
	if !authctx.RequireSuperAdmin(w, r) {
		return
	}
	webutil.Page(w, r, "admin_invite_form.html", map[string]any{
		"ActiveNav": "admin-invite",
	})
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	if !authctx.RequireSuperAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not parse form.", http.StatusBadRequest)
		return
	}

	email := r.PostFormValue("email")
	role := r.PostFormValue("role")
	if role != "super_admin" {
		role = "staff"
	}

	identityID, err := h.kratos.CreateIdentity(r.Context(), email, role)
	if err != nil {
		webutil.UpstreamError(w, r, err, "create identity for invite")
		return
	}

	link, expiresAt, err := h.kratos.CreateRecoveryLink(r.Context(), identityID)
	if err != nil {
		webutil.UpstreamError(w, r, err, "create recovery link for invite")
		return
	}

	webutil.Page(w, r, "admin_invite_created.html", map[string]any{
		"ActiveNav":    "admin-invite",
		"Email":        email,
		"Role":         role,
		"RecoveryLink": link,
		"ExpiresAt":    expiresAt,
	})
}
