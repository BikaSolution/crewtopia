// Command server runs Crewtopia: staff pool, shifts, and roster generation.
package main

import (
	"log"
	"net/http"
	"os"

	"crewtopia/internal/admininvite"
	"crewtopia/internal/authui"
	"crewtopia/internal/db"
	"crewtopia/internal/kratos"
	"crewtopia/internal/roster"
	"crewtopia/internal/seed"
	"crewtopia/internal/shift"
	"crewtopia/internal/staff"
	"crewtopia/internal/webutil"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	dbPath := env("CREWTOPIA_DB", "crewtopia.db")

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	staffStore := staff.NewStore(sqlDB)
	shiftStore := shift.NewStore(sqlDB)
	rosterStore := roster.NewStore(sqlDB, staffStore, shiftStore)

	kratosClient := kratos.NewClient(
		env("CREWTOPIA_KRATOS_PUBLIC_URL", "http://localhost:4433"),
		env("CREWTOPIA_KRATOS_ADMIN_URL", "http://localhost:4434"),
	)

	staffHandlers := staff.NewHandlers(staffStore, shiftStore)
	shiftHandlers := shift.NewHandlers(shiftStore)
	rosterHandlers := roster.NewHandlers(rosterStore, kratosClient)
	seedHandler := seed.NewHandler(staffStore, shiftStore)
	authHandlers := authui.NewHandlers(kratosClient)
	adminInviteHandlers := admininvite.NewHandlers(kratosClient)

	mux := http.NewServeMux()

	// --- Public routes: no session required. Kratos's own public API is no
	// longer proxied here — Oathkeeper's "kratos-public-api" rule handles
	// that hop directly now (see deploy/oathkeeper/access-rules.yml). ---
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	// Some browsers/crawlers request this at the root regardless of the
	// <link rel="icon"> tags in layout.html.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/static/favicon.ico")
	})
	mux.HandleFunc("GET /login", authHandlers.Login)
	mux.HandleFunc("GET /logout", authHandlers.Logout)
	mux.HandleFunc("GET /recovery", authHandlers.Recovery)
	mux.HandleFunc("GET /settings", authHandlers.Settings)
	// The read-only roster is the one page meant to be shared with anyone —
	// staff checking their shifts shouldn't need an account. Everything else
	// (editing, staff/shifts management, CSV export) stays behind login.
	mux.HandleFunc("GET /roster", rosterHandlers.View)
	// The roster is also the front door: root just bounces straight there,
	// for anyone, logged in or not.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/roster", http.StatusSeeOther)
	})

	// --- Everything else requires a session. ---
	protected := http.NewServeMux()

	protected.HandleFunc("GET /staff", staffHandlers.List)
	protected.HandleFunc("GET /staff/new", staffHandlers.NewForm)
	protected.HandleFunc("POST /staff", staffHandlers.Create)
	protected.HandleFunc("GET /staff/{id}/edit", staffHandlers.EditForm)
	protected.HandleFunc("POST /staff/{id}", staffHandlers.Update)
	protected.HandleFunc("POST /staff/{id}/delete", staffHandlers.Delete)

	protected.HandleFunc("GET /shifts", shiftHandlers.List)
	protected.HandleFunc("GET /shifts/new", shiftHandlers.NewForm)
	protected.HandleFunc("POST /shifts", shiftHandlers.Create)
	protected.HandleFunc("GET /shifts/{id}/edit", shiftHandlers.EditForm)
	protected.HandleFunc("POST /shifts/{id}", shiftHandlers.Update)
	protected.HandleFunc("POST /shifts/{id}/delete", shiftHandlers.Delete)

	protected.HandleFunc("GET /roster/edit", rosterHandlers.Edit)
	protected.HandleFunc("POST /roster/generate", rosterHandlers.Generate)
	protected.HandleFunc("POST /roster/assignment/{id}/reassign", rosterHandlers.Reassign)
	protected.HandleFunc("GET /roster/export.csv", rosterHandlers.ExportCSV)

	protected.HandleFunc("GET /admin/invite", adminInviteHandlers.NewForm)
	protected.HandleFunc("POST /admin/invite", adminInviteHandlers.Create)

	protected.HandleFunc("POST /seed", seedHandler.Seed)

	mux.Handle("/", requireTrustedHeaders(webutil.RequireCSRF(protected)))

	addr := env("CREWTOPIA_ADDR", ":8080")
	log.Printf("crewtopia listening on %s", addr)
	// securityHeaders wraps everything, public routes included — the login
	// page is the most obvious clickjacking target in the whole app.
	if err := http.ListenAndServe(addr, securityHeaders(mux)); err != nil {
		log.Fatal(err)
	}
}
