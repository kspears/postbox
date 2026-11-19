package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/kspears/postbox/internal/auth"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/session"
	"github.com/kspears/postbox/internal/store"
)

const (
	errPasskeyUnsupported = "passkey_unsupported" // enrolment: the passkey gave no PRF output
	errPasskeyNoKey       = "passkey_no_key"      // sign-in: the passkey gave no usable PRF output
	errPasskeyFailed      = "passkey_failed"      // the passkey did not verify, or took too long
	errPasskeyRequired    = "passkey_required"
)

// passkeyView is a passkey as Settings lists it.
type passkeyView struct {
	ID    string    `json:"id"`
	Added time.Time `json:"added"`
}

func viewPasskey(p store.Passkey) passkeyView {
	return passkeyView{ID: base64.RawURLEncoding.EncodeToString(p.ID), Added: p.CreatedAt}
}

// passkeyAnswer is the browser's answer to create() or get(), with the PRF output taken out of
// it, base64url.
type passkeyAnswer struct {
	Nonce      string          `json:"nonce"`
	Credential json.RawMessage `json:"credential"`
	PRF        string          `json:"prf"`
}

func (p passkeyAnswer) prf() []byte {
	b, _ := base64.RawURLEncoding.DecodeString(p.PRF)
	return b
}

// passkeyOptions answers POST /api/passkeys/options: what "Add a passkey" gives create().
func (a *apiHandler) passkeyOptions(w http.ResponseWriter, r *http.Request, s *session.Session) {
	nonce, options, err := a.Auth.EnrolOptions(r.Context(), s.Account)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nonce": nonce, "options": options.Response})
}

// addPasskey answers POST /api/passkeys {"nonce", "credential", "prf"}: it stores the new
// passkey with the session's password sealed under its PRF output.
func (a *apiHandler) addPasskey(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req passkeyAnswer
	if !decodeUpTo(w, r, &req, 64<<10) {
		return
	}
	p, err := a.Auth.Enrol(r.Context(), s.Account, s.Password, req.Nonce, req.Credential, req.prf())
	switch {
	case errors.Is(err, auth.ErrNoPRF):
		writeError(w, http.StatusUnprocessableEntity, errPasskeyUnsupported)
	case errors.Is(err, auth.ErrExpired), errors.Is(err, auth.ErrUnknownPasskey):
		log.Printf("add passkey: %v", err)
		writeError(w, http.StatusBadRequest, errPasskeyFailed)
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, http.StatusCreated, viewPasskey(p))
	}
}

// removePasskey answers POST /api/passkeys/delete {"id"} with whether "Require passkey" is
// still on: removing the last passkey turns it off.
func (a *apiHandler) removePasskey(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	id, err := base64.RawURLEncoding.DecodeString(req.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	required, err := a.Store.RemovePasskey(r.Context(), s.Account, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, errNotFound)
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"requirePasskey": required})
	}
}

// requirePasskey answers PUT /api/require-passkey {"on"}. Turning it on needs a passkey and
// ends the account's other password sessions.
func (a *apiHandler) requirePasskey(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		On bool `json:"on"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := a.Store.RequirePasskey(r.Context(), s.Account, req.On, s.ID)
	switch {
	case errors.Is(err, store.ErrNoPasskey):
		writeError(w, http.StatusBadRequest, errBadRequest)
	case err != nil:
		internalError(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// passkeySignInOptions answers POST /api/passkey-session/options: what "Sign in with a passkey"
// gives get().
func (a *apiHandler) passkeySignInOptions(w http.ResponseWriter, r *http.Request) {
	nonce, options, err := a.Auth.SignInOptions()
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nonce": nonce, "options": options.Response})
}

// passkeySignIn answers POST /api/passkey-session {"nonce", "credential", "prf"}: the account
// signed in, or {"newPassword": nonce} when the mail server refused the stored password.
func (a *apiHandler) passkeySignIn(w http.ResponseWriter, r *http.Request) {
	var req passkeyAnswer
	if !decodeUpTo(w, r, &req, 64<<10) {
		return
	}
	account, password, err := a.Auth.SignIn(r.Context(), a.clientIP(r), req.Nonce, req.Credential, req.prf())
	var needed auth.NewPasswordNeeded
	if errors.As(err, &needed) {
		writeJSON(w, http.StatusOK, map[string]string{"newPassword": needed.Nonce})
		return
	}
	if a.signInFailed(w, r, err) {
		return
	}
	a.startSession(w, r, account, password, session.Passkey, true)
}

// passkeyNewPassword answers POST /api/passkey-session/password {"nonce", "password"}: the new
// password for a passkey whose stored one was refused.
func (a *apiHandler) passkeyNewPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nonce    string `json:"nonce"`
		Password string `json:"password"`
	}
	if !decodeUpTo(w, r, &req, 16<<10) {
		return
	}
	account, err := a.Auth.NewPassword(r.Context(), a.clientIP(r), req.Nonce, req.Password)
	if a.signInFailed(w, r, err) {
		return
	}
	a.startSession(w, r, account, req.Password, session.Passkey, true)
}

// signInFailed answers a failed sign-in of any kind, and reports whether it did.
func (a *apiHandler) signInFailed(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, auth.ErrWrongPassword):
		writeError(w, http.StatusUnauthorized, errWrongCredentials)
	case errors.Is(err, auth.ErrThrottled):
		writeError(w, http.StatusTooManyRequests, errThrottled)
	case errors.Is(err, auth.ErrPasskeyRequired):
		writeError(w, http.StatusForbidden, errPasskeyRequired)
	case errors.Is(err, auth.ErrNoPRF):
		writeError(w, http.StatusUnprocessableEntity, errPasskeyNoKey)
	case errors.Is(err, auth.ErrUnknownPasskey), errors.Is(err, auth.ErrExpired):
		writeError(w, http.StatusUnauthorized, errPasskeyFailed)
	case errors.Is(err, mail.ErrUnreachable):
		log.Printf("sign-in: %v", err)
		writeError(w, http.StatusBadGateway, errUnreachable)
	default:
		internalError(w, r, err)
	}
	return true
}

// startSession signs account in on this browser and answers who is signed in.
func (a *apiHandler) startSession(w http.ResponseWriter, r *http.Request, account, password, method string, keep bool) {
	cookie, err := a.Sessions.Create(r.Context(), account, password, method, keep)
	if err != nil {
		internalError(w, r, err)
		return
	}
	http.SetCookie(w, cookie)
	a.Addresses.SeedOnce(mail.Account{ClientIP: a.clientIP(r), Name: account, Password: password})
	writeJSON(w, http.StatusOK, whoAmI{Account: account, Method: method})
}
