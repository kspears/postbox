package api

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/kspears/postbox/internal/auth"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/session"
)

type apiHandler struct {
	Deps
}

// Error codes the browser turns into the spec's messages. The API never sends user-facing text,
// so a translation changes only the frontend.
const (
	errWrongCredentials = "wrong_credentials"
	errThrottled        = "throttled"
	errUnreachable      = "unreachable"
	errSignedOut        = "signed_out"
	errPasswordChanged  = "password_changed"
	errNoFolder         = "no_folder"
	errFolderExists     = "folder_exists"
	errBadRequest       = "bad_request"
	errInternal         = "internal"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func internalError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	writeError(w, http.StatusInternalServerError, errInternal)
}

// clientIP is the person's address: the proxy's header when one is configured and present and
// the connection comes from the trusted proxy, otherwise the connection's. Any other peer could
// write the header itself. The rightmost entry is used because a proxy appends the address it
// saw, and anything to its left came from the client.
func (a *apiHandler) clientIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, _ := netip.ParseAddr(host)
	peer = peer.Unmap().WithZone("")
	if a.ClientIPHeader == "" || !a.TrustedProxy.IsValid() || peer != a.TrustedProxy {
		return peer
	}
	if v := r.Header.Values(a.ClientIPHeader); len(v) > 0 {
		last := v[len(v)-1]
		last = strings.TrimSpace(last[strings.LastIndexByte(last, ',')+1:])
		if ip, err := netip.ParseAddr(last); err == nil {
			return ip.Unmap().WithZone("")
		}
	}
	return peer
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Keep     bool   `json:"keep"`
}

func (a *apiHandler) signIn(w http.ResponseWriter, r *http.Request) {
	var req signInRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	account := auth.Account(req.Email)
	err := a.Auth.PasswordSignIn(r.Context(), a.clientIP(r), account, req.Password)
	if a.signInFailed(w, r, err) {
		return
	}
	a.startSession(w, r, account, req.Password, session.Password, req.Keep)
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
	cookie, confirm, err := a.Sessions.Create(r.Context(), account, password, method, keep)
	if err != nil {
		internalError(w, r, err)
		return
	}
	http.SetCookie(w, cookie)
	a.Addresses.SeedOnce(mail.Account{ClientIP: a.clientIP(r), Name: account, Password: password})
	writeJSON(w, http.StatusOK, whoAmI{Account: account, Method: method, Confirm: confirm})
}

type sessionHandler func(http.ResponseWriter, *http.Request, *session.Session)

// withSession runs next for a signed-in request, and answers 401 otherwise. The 401 also
// removes a dead cookie from the browser.
func (a *apiHandler) withSession(next sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, err := a.Sessions.FromRequest(r)
		if errors.Is(err, session.ErrNoSession) {
			if clear := a.Sessions.Clear(); hasCookie(r, clear.Name) {
				http.SetCookie(w, clear)
			}
			writeError(w, http.StatusUnauthorized, errSignedOut)
			return
		}
		if err != nil {
			internalError(w, r, err)
			return
		}
		next(w, r, s)
	}
}

func hasCookie(r *http.Request, name string) bool {
	_, err := r.Cookie(name)
	return err == nil
}

type whoAmI struct {
	Account string `json:"account"`
	Method  string `json:"method"`
	// Confirm, at sign-in only, is the token that lets this tab change passkeys for a while.
	Confirm string `json:"confirm,omitempty"`
}

func (a *apiHandler) whoAmI(w http.ResponseWriter, _ *http.Request, s *session.Session) {
	writeJSON(w, http.StatusOK, whoAmI{Account: s.Account, Method: s.Method})
}

// signOut ends this session, or with everywhere every session of the account.
func (a *apiHandler) signOut(everywhere bool) sessionHandler {
	end := a.Sessions.End
	if everywhere {
		end = a.Sessions.EndAll
	}
	return func(w http.ResponseWriter, r *http.Request, s *session.Session) {
		cookie, err := end(r.Context(), s)
		if err != nil {
			internalError(w, r, err)
			return
		}
		http.SetCookie(w, cookie)
		w.WriteHeader(http.StatusNoContent)
	}
}
