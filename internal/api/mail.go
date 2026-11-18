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

// conversations answers GET /api/conversations?folder=<name>[&cursor=<uid>].
func (a *apiHandler) conversations(w http.ResponseWriter, r *http.Request, s *session.Session) {
	folder := r.URL.Query().Get("folder")
	cursor, err := strconv.ParseUint(r.URL.Query().Get("cursor"), 10, 32)
	if folder == "" || (err != nil && r.URL.Query().Has("cursor")) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var page *mail.Page
	missing := false
	ok := a.withMail(w, r, s, func(c *mail.Conn) error {
		if _, err := c.Select(folder); err != nil {
			// The server's NO to SELECT: no such folder, or one that cannot be opened.
			var status *imap.Error
			missing = errors.As(err, &status) && status.Type == imap.StatusResponseTypeNo
			if missing {
				return nil
			}
			return err
		}
		var err error
		page, err = c.Conversations(folder, imap.UID(cursor))
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
