package api

import (
	"errors"
	"net/http"
	netmail "net/mail"
	"slices"
	"strings"

	"github.com/kspears/postbox/internal/render"
	"github.com/kspears/postbox/internal/session"
	"github.com/kspears/postbox/internal/store"
)

// Preferences are the person's choices that are not about one folder.
type preferences struct {
	// Appearance is "system", "light" or "dark".
	Appearance string `json:"appearance"`
	// Composer is how a new message starts: "rich" or "plain".
	Composer string `json:"composer"`
}

const preferencesKey = "preferences"

func (a *apiHandler) preferences(r *http.Request, account string) (preferences, error) {
	p := preferences{Appearance: "system", Composer: "rich"}
	err := a.Store.Setting(r.Context(), account, preferencesKey, &p)
	return p, err
}

// settings answers GET /api/settings: the preferences, the identities, the passkeys and
// whether one is required.
func (a *apiHandler) settings(w http.ResponseWriter, r *http.Request, s *session.Session) {
	p, err := a.preferences(r, s.Account)
	if err != nil {
		internalError(w, r, err)
		return
	}
	ids, err := a.Store.Identities(r.Context(), s.Account)
	if err != nil {
		internalError(w, r, err)
		return
	}
	keys, err := a.Store.Passkeys(r.Context(), s.Account)
	if err != nil {
		internalError(w, r, err)
		return
	}
	required, err := a.Store.PasskeyRequired(r.Context(), s.Account)
	if err != nil {
		internalError(w, r, err)
		return
	}
	passkeys := make([]passkeyView, len(keys))
	for i, k := range keys {
		passkeys[i] = viewPasskey(k)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"preferences": p, "identities": ids, "passkeys": passkeys, "requirePasskey": required,
	})
}

// updatePreferences answers PUT /api/preferences with the whole set of preferences.
func (a *apiHandler) updatePreferences(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var p preferences
	if !decode(w, r, &p) ||
		!slices.Contains([]string{"system", "light", "dark"}, p.Appearance) ||
		!slices.Contains([]string{"rich", "plain"}, p.Composer) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if err := a.Store.SetSetting(r.Context(), s.Account, preferencesKey, p); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// saveIdentity answers POST /api/identities {"id"?, "name", "email", "signature"}: it adds an
// identity (no id) or changes one, and returns it as stored, its signature sanitised.
func (a *apiHandler) saveIdentity(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var id store.Identity
	if !decode(w, r, &id) {
		return
	}
	id.Name = strings.TrimSpace(id.Name)
	id.Email = strings.TrimSpace(id.Email)
	parsed, err := netmail.ParseAddress(id.Email)
	if err != nil || parsed.Address != id.Email || len(id.Name) > 200 || strings.ContainsAny(id.Name, "\r\n") || len(id.Signature) > 64<<10 {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	id.Signature = render.Composed(id.Signature)
	saved, err := a.Store.SaveIdentity(r.Context(), s.Account, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, errNotFound)
	case errors.Is(err, store.ErrLoginIdentity):
		writeError(w, http.StatusBadRequest, errBadRequest)
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, saved)
	}
}

// deleteIdentity answers POST /api/identities/delete {"id"}. The login address stays.
func (a *apiHandler) deleteIdentity(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := a.Store.DeleteIdentity(r.Context(), s.Account, req.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, errNotFound)
	case errors.Is(err, store.ErrLoginIdentity):
		writeError(w, http.StatusBadRequest, errBadRequest)
	case err != nil:
		internalError(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
