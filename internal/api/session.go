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

// clientIP is the person's address: the proxy's header when one is configured and present,
// otherwise the connection's. The rightmost entry is used because a proxy appends the address
// it saw, and anything to its left came from the client.
func (a *apiHandler) clientIP(r *http.Request) netip.Addr {
	if a.ClientIPHeader != "" {
		if v := r.Header.Values(a.ClientIPHeader); len(v) > 0 {
			last := v[len(v)-1]
			last = strings.TrimSpace(last[strings.LastIndexByte(last, ',')+1:])
			if ip, err := netip.ParseAddr(last); err == nil {
				return ip.Unmap()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, _ := netip.ParseAddr(host)
	return ip.Unmap()
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
	err := a.Auth.CheckPassword(r.Context(), a.clientIP(r), account, req.Password)
	switch {
	case errors.Is(err, auth.ErrWrongPassword):
		writeError(w, http.StatusUnauthorized, errWrongCredentials)
		return
	case errors.Is(err, auth.ErrThrottled):
		writeError(w, http.StatusTooManyRequests, errThrottled)
		return
	case errors.Is(err, mail.ErrUnreachable):
		log.Printf("sign-in: %v", err)
		writeError(w, http.StatusBadGateway, errUnreachable)
		return
	case err != nil:
		internalError(w, r, err)
		return
	}
	cookie, err := a.Sessions.Create(r.Context(), account, req.Password, session.Password, req.Keep)
	if err != nil {
		internalError(w, r, err)
		return
	}
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, whoAmI{Account: account, Method: session.Password})
}

type sessionHandler func(http.ResponseWriter, *http.Request, *session.Session)

// withSession runs next for a signed-in request, and answers 401 otherwise. The 401 also
// removes a dead cookie from the browser.
func (a *apiHandler) withSession(next sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, err := a.Sessions.FromRequest(r)
		if errors.Is(err, session.ErrNoSession) {
			if _, cookieErr := r.Cookie(a.Sessions.Clear().Name); cookieErr == nil {
				http.SetCookie(w, a.Sessions.Clear())
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

type whoAmI struct {
	Account string `json:"account"`
	Method  string `json:"method"`
}

func (a *apiHandler) whoAmI(w http.ResponseWriter, _ *http.Request, s *session.Session) {
	writeJSON(w, http.StatusOK, whoAmI{Account: s.Account, Method: s.Method})
}

func (a *apiHandler) signOut(w http.ResponseWriter, r *http.Request, s *session.Session) {
	cookie, err := a.Sessions.End(r.Context(), s)
	if err != nil {
		internalError(w, r, err)
		return
	}
	http.SetCookie(w, cookie)
	w.WriteHeader(http.StatusNoContent)
}

func (a *apiHandler) signOutEverywhere(w http.ResponseWriter, r *http.Request, s *session.Session) {
	cookie, err := a.Sessions.EndAll(r.Context(), s)
	if err != nil {
		internalError(w, r, err)
		return
	}
	http.SetCookie(w, cookie)
	w.WriteHeader(http.StatusNoContent)
}
