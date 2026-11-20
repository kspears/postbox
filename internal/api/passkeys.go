package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/kspears/postbox/internal/auth"
	"github.com/kspears/postbox/internal/session"
	"github.com/kspears/postbox/internal/store"
)

const (
	errPasskeyUnsupported = "passkey_unsupported" // enrolment: the passkey gave no PRF output
	errPasskeyNoKey       = "passkey_no_key"      // sign-in: the passkey gave no usable PRF output
	errPasskeyFailed      = "passkey_failed"      // the passkey did not verify, or took too long
	errPasskeyRequired    = "passkey_required"
	errConfirmNeeded      = "confirm_needed" // enter the password again first
)

// confirmHeader carries the confirmation token the tab got at sign-in or by entering the
// password again.
const confirmHeader = "X-Postbox-Confirm"

// withConfirmed runs next only for a tab that entered the password recently, and answers 403
// confirm_needed otherwise: changing passkeys must take more than a stolen cookie.
func (a *apiHandler) withConfirmed(next sessionHandler) sessionHandler {
	return func(w http.ResponseWriter, r *http.Request, s *session.Session) {
		if !a.Sessions.Confirmed(s, r.Header.Get(confirmHeader)) {
			writeError(w, http.StatusForbidden, errConfirmNeeded)
			return
		}
		next(w, r, s)
	}
}

// confirmPassword answers POST /api/session/confirm {"password"} with {"confirm": token}: the
// password entered again, which lets the tab holding the token change passkeys and "Require
// passkey" for a while.
func (a *apiHandler) confirmPassword(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Password string `json:"password"`
	}
	if !decodeUpTo(w, r, &req, 16<<10) {
		return
	}
	err := a.Auth.ConfirmPassword(r.Context(), a.clientIP(r), s.Account, req.Password, s.Password)
	if a.signInFailed(w, r, err) {
		return
	}
	token, err := a.Sessions.Confirm(r.Context(), s)
	if errors.Is(err, session.ErrNoSession) {
		writeError(w, http.StatusUnauthorized, errSignedOut)
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"confirm": token})
}

// passkeyView is a passkey as Settings lists it. Lock is store.LockPRF or store.LockServer, so
// a passkey that fell back to the server's key is visible as one.
type passkeyView struct {
	ID    protocol.URLEncodedBase64 `json:"id"`
	Added time.Time                 `json:"added"`
	Lock  string                    `json:"lock"`
}

func viewOf(p store.Passkey) passkeyView {
	return passkeyView{ID: p.ID, Added: p.CreatedAt, Lock: p.Lock}
}

// passkeyAnswer is the browser's answer to create() or get(), with the PRF output taken out of
// it. A malformed PRF output is a bad request, never mistaken for none.
type passkeyAnswer struct {
	Nonce      string                    `json:"nonce"`
	Credential json.RawMessage           `json:"credential"`
	PRF        protocol.URLEncodedBase64 `json:"prf"`
}

// passkeys answers GET /api/passkeys: the account's passkeys and whether one is required.
func (a *apiHandler) passkeys(w http.ResponseWriter, r *http.Request, s *session.Session) {
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
	list := make([]passkeyView, len(keys))
	for i, k := range keys {
		list[i] = viewOf(k)
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": list, "requirePasskey": required})
}

// passkeyOptions answers POST /api/passkeys/options: what "Add a passkey" gives create().
func (a *apiHandler) passkeyOptions(w http.ResponseWriter, r *http.Request, s *session.Session) {
	nonce, options, err := a.Auth.EnrolOptions(r.Context(), s.Account)
	if a.ceremonyFailed(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nonce": nonce, "options": options.Response})
}

// addPasskey answers POST /api/passkeys {"nonce", "credential", "prf"}: it stores the new
// passkey with the session's password sealed under its PRF output.
func (a *apiHandler) addPasskey(w http.ResponseWriter, r *http.Request, s *session.Session) {
	// Not behind withConfirmed: the nonce came from passkeyOptions, which is, and is this
	// account's alone. Checking again would lose a passkey whose prompt ran past the window.
	var req passkeyAnswer
	if !decodeUpTo(w, r, &req, 64<<10) {
		return
	}
	p, err := a.Auth.Enrol(r.Context(), s.Account, s.Password, req.Nonce, req.Credential, req.PRF)
	switch {
	case errors.Is(err, auth.ErrNoPRF):
		writeError(w, http.StatusUnprocessableEntity, errPasskeyUnsupported)
	case errors.Is(err, auth.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, errInternal)
	case errors.Is(err, auth.ErrExpired), errors.Is(err, auth.ErrUnknownPasskey):
		log.Printf("add passkey: %v", err)
		writeError(w, http.StatusBadRequest, errPasskeyFailed)
	case err != nil:
		internalError(w, r, err)
	default:
		if p.Lock == store.LockServer {
			log.Printf("add passkey: %s's new passkey gave no PRF output; the server's key locks its password", s.Account)
		}
		writeJSON(w, http.StatusCreated, viewOf(p))
	}
}

// removePasskey answers POST /api/passkeys/delete {"id"} with whether "Require passkey" is
// still on: removing the last passkey turns it off. The account's other passkey sessions end.
func (a *apiHandler) removePasskey(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		ID protocol.URLEncodedBase64 `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	required, err := a.Store.RemovePasskey(r.Context(), s.Account, req.ID, s.ID)
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
	if a.ceremonyFailed(w, r, err) {
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
	account, password, err := a.Auth.SignIn(r.Context(), a.clientIP(r), req.Nonce, req.Credential, req.PRF)
	var needed auth.NewPasswordNeeded
	if errors.As(err, &needed) {
		writeJSON(w, http.StatusOK, map[string]string{"newPassword": needed.Nonce})
		return
	}
	if a.signInFailed(w, r, err) {
		return
	}
	a.startSession(w, r, account, password, session.Passkey, false)
}

// ceremonyFailed answers a passkey ceremony that could not start, and reports whether it did.
func (a *apiHandler) ceremonyFailed(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, auth.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, errInternal)
	default:
		internalError(w, r, err)
	}
	return true
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
	a.startSession(w, r, account, req.Password, session.Passkey, false)
}
