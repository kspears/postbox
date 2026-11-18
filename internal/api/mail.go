package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/emersion/go-imap/v2"

	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/session"
)

// withMail runs fn on one of the account's IMAP connections and answers the request itself when
// that fails. It reports whether fn ran without error.
//
// The mail server refusing the session's password means it was changed elsewhere: the session
// ends and the browser is told why. Any other failure to reach the server leaves the session
// alone.
func (a *apiHandler) withMail(w http.ResponseWriter, r *http.Request, s *session.Session, fn func(*mail.Conn) error) bool {
	acct := mail.Account{ClientIP: a.clientIP(r), Name: s.Account, Password: s.Password}
	err := a.Mail.Do(r.Context(), acct, fn)
	switch {
	case err == nil:
		return true
	case errors.Is(err, mail.ErrRefused):
		if clear, endErr := a.Sessions.End(r.Context(), s); endErr == nil {
			http.SetCookie(w, clear)
		}
		writeError(w, http.StatusUnauthorized, errPasswordChanged)
	case errors.Is(err, mail.ErrUnreachable):
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		writeError(w, http.StatusServiceUnavailable, errUnreachable)
	case r.Context().Err() != nil:
		// The browser went away; nobody is listening for an answer.
	default:
		internalError(w, r, err)
	}
	return false
}

func (a *apiHandler) folders(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var folders []mail.Folder
	ok := a.withMail(w, r, s, func(c *mail.Conn) (err error) {
		folders, err = c.Folders()
		return err
	})
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
	}
}

// conversations answers GET /api/conversations?folder=<name>[&cursor=<uid>&shown=<n>], where
// shown is how many conversations the browser already lists.
func (a *apiHandler) conversations(w http.ResponseWriter, r *http.Request, s *session.Session) {
	q := r.URL.Query()
	folder := q.Get("folder")
	cursor, cursorErr := strconv.ParseUint(q.Get("cursor"), 10, 32)
	shown, shownErr := strconv.Atoi(q.Get("shown"))
	if folder == "" || (cursorErr != nil && q.Has("cursor")) || (shownErr != nil && q.Has("shown")) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var page *mail.Page
	missing := false
	ok := a.withMail(w, r, s, func(c *mail.Conn) error {
		var err error
		page, err = c.Conversations(folder, imap.UID(cursor), shown)
		if mail.IsMissingFolder(err) {
			missing = true
			return nil
		}
		return err
	})
	switch {
	case !ok:
	case missing:
		writeError(w, http.StatusNotFound, errNoFolder)
	default:
		writeJSON(w, http.StatusOK, page)
	}
}
