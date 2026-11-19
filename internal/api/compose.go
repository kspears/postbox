package api

import (
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/kspears/postbox/internal/compose"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/outbox"
	"github.com/kspears/postbox/internal/session"
	"github.com/kspears/postbox/internal/store"
)

const errTooLate = "too_late"

// What a composer opens with.
type composerView struct {
	Draft      *compose.Draft   `json:"draft"`
	Identities []compose.Person `json:"identities"`
	// SizeLimit is the largest message the submission server accepts, in bytes; 0 when unknown.
	SizeLimit int64 `json:"sizeLimit"`
}

// sizeLimit asks the submission server once per account and remembers the answer.
type sizeLimits struct {
	mu     sync.Mutex
	limits map[string]int64
}

func (l *sizeLimits) get(r *http.Request, a *apiHandler, s *session.Session) int64 {
	l.mu.Lock()
	n, ok := l.limits[s.Account]
	l.mu.Unlock()
	if ok {
		return n
	}
	n, err := a.Submit.SizeLimit(r.Context(), a.clientIP(r), s.Account, s.Password)
	if err != nil {
		return 0
	}
	l.mu.Lock()
	if l.limits == nil {
		l.limits = map[string]int64{}
	}
	l.limits[s.Account] = n
	l.mu.Unlock()
	return n
}

func me(s *session.Session) compose.Person { return compose.Person{Email: s.Account} }

// composer answers GET /api/compose?mode=new|reply|replyAll|forward|draft[&folder=&uid=]: the
// content a composer opens with.
func (a *apiHandler) composer(w http.ResponseWriter, r *http.Request, s *session.Session) {
	q := r.URL.Query()
	mode, folder := q.Get("mode"), q.Get("folder")
	uid, hasUID := uidParam(r, "uid")
	modes := []string{"new", compose.ModeReply, compose.ModeReplyAll, compose.ModeForward, "draft"}
	if !slices.Contains(modes, mode) || (mode != "new" && (!hasUID || (folder == "" && mode != "draft"))) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	view := composerView{Identities: []compose.Person{me(s)}}
	if mode == "new" {
		view.Draft = &compose.Draft{From: me(s), To: []compose.Person{}, Cc: []compose.Person{}, Bcc: []compose.Person{}, Attachments: []compose.Attachment{}}
	} else if !a.withMail(w, r, s, func(c *mail.Conn) error {
		if mode == "draft" && folder == "" {
			var err error
			if folder, err = c.RoleFolder(mail.RoleDrafts); err != nil {
				return err
			}
		}
		raw, err := c.Raw(folder, uid)
		if err != nil {
			return err
		}
		if mode == "draft" {
			view.Draft, err = compose.FromDraft(raw, folder, uint32(uid))
			if err == nil {
				view.Draft.From = me(s)
			}
			return err
		}
		original, err := compose.Parse(raw, folder, uint32(uid))
		if err != nil {
			return err
		}
		view.Draft = compose.Respond(original, mode, me(s))
		return nil
	}) {
		return
	}
	view.SizeLimit = a.sizes.get(r, a, s)
	writeJSON(w, http.StatusOK, view)
}

// readDraft reads a composer's content from the request and makes it the account's own: the
// sender is the account (identities come later) and the rest is checked for size.
func readDraft(w http.ResponseWriter, r *http.Request, s *session.Session) (*compose.Draft, bool) {
	var d compose.Draft
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if !decode(w, r, &d) {
		return nil, false
	}
	d.From = me(s)
	if len(d.Subject) > 1000 || len(d.To)+len(d.Cc)+len(d.Bcc) > 500 {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return nil, false
	}
	return &d, true
}

// saveDraft builds d and stores it in Drafts in place of its previous copy.
func saveDraft(c *mail.Conn, d *compose.Draft) (string, imap.UID, error) {
	raw, err := compose.Build(d, time.Now(), true, func(at compose.Attachment) ([]byte, error) {
		return c.Part(at.Folder, imap.UID(at.UID), at.Part)
	})
	if err != nil {
		return "", 0, err
	}
	return c.SaveDraft(raw, imap.UID(d.UID))
}

// saveDraftHandler answers POST /api/drafts with the composer's content: it saves the draft and
// returns where it is now.
func (a *apiHandler) saveDraftHandler(w http.ResponseWriter, r *http.Request, s *session.Session) {
	d, ok := readDraft(w, r, s)
	if !ok {
		return
	}
	var folder string
	var uid imap.UID
	if a.withMail(w, r, s, func(c *mail.Conn) (err error) {
		folder, uid, err = saveDraft(c, d)
		return err
	}) {
		writeJSON(w, http.StatusOK, map[string]any{"folder": folder, "uid": uid, "messageId": d.MessageID})
	}
}

// discardDraft answers POST /api/drafts/discard {"uid"}.
func (a *apiHandler) discardDraft(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		UID imap.UID `json:"uid"`
	}
	if !decode(w, r, &req) || req.UID == 0 {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if a.withMail(w, r, s, func(c *mail.Conn) error { return c.DiscardDraft("", req.UID) }) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// send answers POST /api/send with the composer's content: the message is saved to Drafts and
// enters the outbox's undo window. It returns the send's id.
func (a *apiHandler) send(w http.ResponseWriter, r *http.Request, s *session.Session) {
	d, ok := readDraft(w, r, s)
	if !ok {
		return
	}
	if len(d.Recipients()) == 0 {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var folder string
	var uid imap.UID
	if !a.withMail(w, r, s, func(c *mail.Conn) (err error) {
		folder, uid, err = saveDraft(c, d)
		return err
	}) {
		return
	}
	id, err := a.Outbox.Queue(r.Context(), a.account(r, s), folder, uid)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "folder": folder, "uid": uid})
}

// undoSend answers POST /api/send/undo {"id"}: within the window, the message stays a draft.
func (a *apiHandler) undoSend(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) || req.ID == "" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	send, err := a.Outbox.Undo(r.Context(), s.Account, req.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, errNotFound)
	case errors.Is(err, outbox.ErrTooLate):
		writeError(w, http.StatusConflict, errTooLate)
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"folder": send.DraftsFolder, "uid": send.DraftUID})
	}
}

// sendStatus answers GET /api/send?id=: what has become of a send.
func (a *apiHandler) sendStatus(w http.ResponseWriter, r *http.Request, s *session.Session) {
	send, err := a.Outbox.Status(r.Context(), s.Account, r.URL.Query().Get("id"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, errNotFound)
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, send)
	}
}

// notices answers GET /api/notices: sends whose outcome the person has not been told.
func (a *apiHandler) notices(w http.ResponseWriter, r *http.Request, s *session.Session) {
	sends, err := a.Store.SendsIn(r.Context(), s.Account, outbox.Failed, outbox.Unsent, outbox.Unknown, outbox.SentNoCopy)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if sends == nil {
		sends = []store.Send{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"notices": sends})
}

// dismissNotice answers POST /api/notices/dismiss {"id"}: the person has been told.
func (a *apiHandler) dismissNotice(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) || req.ID == "" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if err := a.Store.DeleteSend(r.Context(), s.Account, req.ID); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
