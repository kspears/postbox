// Package api is Postbox's HTTP surface: the health check, the browser-facing API and the
// embedded frontend.
package api

import (
	"context"
	"io/fs"
	"net/http"
	"net/netip"
	"strings"

	"github.com/kspears/postbox/internal/addressbook"
	"github.com/kspears/postbox/internal/auth"
	"github.com/kspears/postbox/internal/clock"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/outbox"
	"github.com/kspears/postbox/internal/session"
	"github.com/kspears/postbox/internal/store"
)

// Deps is what the handlers need.
type Deps struct {
	Sessions *session.Manager
	Auth     *auth.Authenticator
	Mail     *mail.Pool
	Watch    *mail.Watcher
	Submit   *mail.Submitter
	Outbox   *outbox.Outbox
	// Addresses remembers who the person has sent mail to, for autocomplete.
	Addresses *addressbook.Book
	// Stopping ends when Postbox is shutting down, so open event streams can end and let it.
	Stopping context.Context
	Store    *store.Store
	Clock    *clock.Clock
	// Origin is the public URL; state-changing requests must come from it.
	Origin string
	// ClientIPHeader, when set, names the reverse proxy's header with the person's address.
	ClientIPHeader string
	// TrustedProxy is the one peer whose ClientIPHeader is believed.
	TrustedProxy netip.Addr
}

// NewHandler serves the frontend in dist and the endpoints beside it.
func NewHandler(dist fs.FS, d Deps) http.Handler {
	a := &apiHandler{Deps: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /api/session", a.signIn)
	mux.HandleFunc("GET /api/session", a.withSession(a.whoAmI))
	mux.HandleFunc("POST /api/passkey-session/options", a.passkeySignInOptions)
	mux.HandleFunc("POST /api/passkey-session", a.passkeySignIn)
	mux.HandleFunc("POST /api/passkey-session/password", a.passkeyNewPassword)
	mux.HandleFunc("DELETE /api/session", a.withSession(a.signOut(false)))
	mux.HandleFunc("DELETE /api/sessions", a.withSession(a.signOut(true)))
	mux.HandleFunc("POST /api/session/confirm", a.withSession(a.confirmPassword))
	mux.HandleFunc("GET /api/folders", a.withSession(a.folders))
	mux.HandleFunc("GET /api/conversations", a.withSession(a.conversations))
	mux.HandleFunc("GET /api/conversation", a.withSession(a.conversation))
	mux.HandleFunc("GET /api/body", a.withSession(a.body))
	mux.HandleFunc("GET /api/attachment", a.withSession(a.attachment))
	mux.HandleFunc("POST /api/seen", a.withSession(a.markRead))
	mux.HandleFunc("POST /api/images/allow", a.withSession(a.allowImages))
	mux.HandleFunc("GET /api/events", a.withSession(a.events))
	mux.HandleFunc("POST /api/move", a.withSession(a.move))
	mux.HandleFunc("POST /api/expunge", a.withSession(a.expunge))
	mux.HandleFunc("POST /api/flags", a.withSession(a.flags))
	mux.HandleFunc("POST /api/folders", a.withSession(a.createFolder))
	mux.HandleFunc("POST /api/folders/rename", a.withSession(a.renameFolder))
	mux.HandleFunc("POST /api/folders/delete", a.withSession(a.deleteFolder))
	mux.HandleFunc("PUT /api/folder-order", a.withSession(a.folderOrder))
	mux.HandleFunc("GET /api/compose", a.withSession(a.composer))
	mux.HandleFunc("POST /api/drafts", a.withSession(a.saveDraftHandler))
	mux.HandleFunc("POST /api/drafts/discard", a.withSession(a.discardDraft))
	mux.HandleFunc("POST /api/send", a.withSession(a.send))
	mux.HandleFunc("POST /api/send/undo", a.withSession(a.undoSend))
	mux.HandleFunc("GET /api/send", a.withSession(a.sendStatus))
	mux.HandleFunc("GET /api/addresses", a.withSession(a.addresses))
	mux.HandleFunc("GET /api/search", a.withSession(a.search))
	mux.HandleFunc("GET /api/notices", a.withSession(a.notices))
	mux.HandleFunc("POST /api/notices/dismiss", a.withSession(a.dismissNotice))
	mux.HandleFunc("GET /api/settings", a.withSession(a.settings))
	mux.HandleFunc("PUT /api/preferences", a.withSession(a.updatePreferences))
	mux.HandleFunc("POST /api/identities", a.withSession(a.saveIdentity))
	mux.HandleFunc("POST /api/identities/delete", a.withSession(a.deleteIdentity))
	mux.HandleFunc("GET /api/passkeys", a.withSession(a.passkeys))
	mux.HandleFunc("POST /api/passkeys/options", a.withSession(a.withConfirmed(a.passkeyOptions)))
	mux.HandleFunc("POST /api/passkeys", a.withSession(a.addPasskey))
	mux.HandleFunc("POST /api/passkeys/delete", a.withSession(a.withConfirmed(a.removePasskey)))
	mux.HandleFunc("PUT /api/require-passkey", a.withSession(a.withConfirmed(a.requirePasskey)))
	testRoutes(mux, a)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", frontend(dist))
	return securityHeaders(sameOrigin(d.Origin, mux))
}

// securityHeaders applies to every response. The app's own policy allows only its own files;
// message bodies will get a stricter one of their own.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin refuses state-changing API requests that do not come from Postbox's own pages.
// SameSite=Lax cookies already stop most cross-site requests; this closes the rest.
func sameOrigin(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead
		if !safe && strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Origin") != origin {
			writeError(w, http.StatusForbidden, "cross_origin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// frontend serves the built files, and index.html for every other path so the single-page
// app's own routes survive a reload.
func frontend(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	index, indexErr := fs.ReadFile(dist, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if info, err := fs.Stat(dist, name); name != "" && err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// Vite puts a content hash in every name under assets/.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			// A tab still running the previous release asks for files that are gone. It
			// must get a 404, not the app's HTML with a script's URL.
			http.NotFound(w, r)
			return
		}
		if indexErr != nil {
			http.Error(w, "The frontend is not built. Run: just build", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
