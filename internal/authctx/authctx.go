// Package authctx carries the current request's identity through
// context.Context. How that identity gets established (a direct Kratos
// whoami call in Phase 6, trusted Oathkeeper-injected headers from Phase 7
// onward) is an implementation detail of whichever middleware populates it —
// handlers downstream only ever read User from here.
package authctx

import (
	"context"
	"net/http"
)

type User struct {
	ID    string
	Email string
	Role  string
}

func (u User) IsSuperAdmin() bool { return u.Role == "super_admin" }

type contextKey struct{}

func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, contextKey{}, u)
}

func FromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(contextKey{}).(User)
	return u, ok
}

// RequireSuperAdmin is the shared app-level enforcement behind every
// super-admin-only action (inviting logins, seeding sample data) — defense
// in depth alongside Oathkeeper, which only checks that a route has a valid
// session at all, not which role it belongs to. Writes a 403 and returns
// false if the caller isn't a super admin.
func RequireSuperAdmin(w http.ResponseWriter, r *http.Request) bool {
	u, ok := FromContext(r.Context())
	if !ok || !u.IsSuperAdmin() {
		http.Error(w, "forbidden: super admin only", http.StatusForbidden)
		return false
	}
	return true
}
