package api

import (
	"context"
	"errors"
	"net/http"
	netmail "net/mail"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/kspears/postbox/internal/compose"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/outbox"
	"github.com/kspears/postbox/internal/session"
	"github.com/kspears/postbox/internal/store"
)

const (
	errTooLate = "too_late"
	// errSending: the draft is in its undo window; "Undo" is the way back to it.
	errSending = "sending"
)

// What a composer opens with.
type composerView struct {
	Draft      *compose.Draft   `json:"draft"`
	Identities []store.Identity `json:"identities"`
	// SizeLimit is the largest message the submission server accepts, in bytes; 0 when unknown.
	SizeLimit int64 `json:"sizeLimit"`
}

// people is identities as senders, the login address first.
func people(ids []store.Identity) []compose.Person {
	out := make([]compose.Person, len(ids))
	for i, id := range ids {
		out[i] = compose.Person{Name: id.Name, Email: id.Email}
	}
	return out
}

// sender is the identity d is from, or the login address when d names none of them.
func sender(ids []store.Identity, from compose.Person) compose.Person {
	id := ids[0]
	if i := slices.IndexFunc(ids, func(id store.Identity) bool { return strings.EqualFold(id.Email, from.Email) }); i >= 0 {
		id = ids[i]
	}
	return compose.Person{Name: id.Name, Email: id.Email}
}

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
	ids, err := a.Store.Identities(r.Context(), s.Account)
	if err != nil {
		internalError(w, r, err)
		return
	}
	view := composerView{Identities: ids}
	if mode == "new" {
		view.Draft = compose.NewDraft(people(ids)[0])
	} else if !a.withMail(w, r, s, func(c *mail.Conn) error {
		if mode == "draft" && folder == "" {
			var err error
			if folder, err = c.RoleFolder(mail.RoleDrafts); err != nil {
				return err
			}
		}
		if mode == "draft" {
			// Saving the draft from a composer would replace the copy the outbox is about to send.
			waiting, err := a.Store.SendWaiting(r.Context(), s.Account, folder, uint32(uid))
			if err != nil {
				return err
			}
			if waiting {
				return errDraftSending
			}
		}
		raw, err := c.Raw(folder, uid)
		if err != nil {
			return err
		}
		if mode == "draft" {
			view.Draft, err = compose.FromDraft(raw, folder, uint32(uid))
			if err == nil {
				view.Draft.From = sender(ids, view.Draft.From)
			}
			return err
		}
		original, err := compose.Parse(raw, folder, uint32(uid))
		if err != nil {
			return err
		}
		view.Draft = compose.Respond(original, mode, people(ids))
		return nil
	}) {
		return
	}
	view.SizeLimit = a.Submit.SizeLimit(r.Context())
	writeJSON(w, http.StatusOK, view)
}

var errDraftSending = errors.New("the draft is being sent")

// readDraft reads a composer's content from the request and makes it the account's own: the
// sender is one of the account's identities, and the rest is checked for size and for
// attachments that name a real part. The request may carry files the person added, base64
// encoded, up to well past any submission server's limit.
func (a *apiHandler) readDraft(w http.ResponseWriter, r *http.Request, s *session.Session) (*compose.Draft, bool) {
	var d compose.Draft
	if !decodeUpTo(w, r, &d, 160<<20) {
		return nil, false
	}
	ids, err := a.Store.Identities(r.Context(), s.Account)
	if err != nil {
		internalError(w, r, err)
		return nil, false
	}
	d.From = sender(ids, d.From)
	ok := len(d.Subject) <= 1000 && len(d.To)+len(d.Cc)+len(d.Bcc) <= 500
	for _, p := range slices.Concat(d.To, d.Cc, d.Bcc) {
		parsed, err := netmail.ParseAddress(p.Email)
		ok = ok && err == nil && parsed.Address == p.Email && parsed.Name == ""
	}
	for _, at := range d.Attachments {
		ok = ok && (at.Data != nil || (at.Folder != "" && at.UID != 0 && validPart(at.Part)))
	}
	if !ok {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return nil, false
	}
	return &d, true
}

// saveDraft builds d and stores it in Drafts in place of its previous copy, answering the
// request itself when that fails. It returns where the draft is now; d's attachments then
// point into that copy, as the one they came from may be gone.
func (a *apiHandler) saveDraft(w http.ResponseWriter, r *http.Request, s *session.Session, d *compose.Draft) (folder string, uid imap.UID, ok bool) {
	ok = a.withMail(w, r, s, func(c *mail.Conn) error {
		raw, err := compose.Build(d, time.Now(), func(at compose.Attachment) ([]byte, error) {
			return c.Part(at.Folder, imap.UID(at.UID), at.Part)
		})
		if err != nil {
			return err
		}
		folder, uid, err = c.SaveDraft(raw, imap.UID(d.UID))
		if err == nil {
			d.Attachments = compose.Saved(d, folder, uint32(uid))
		}
		return err
	})
	return folder, uid, ok
}

// saveDraftHandler answers POST /api/drafts with the composer's content: it saves the draft and
// returns where it is now.
func (a *apiHandler) saveDraftHandler(w http.ResponseWriter, r *http.Request, s *session.Session) {
	d, ok := a.readDraft(w, r, s)
	if !ok {
		return
	}
	if _, uid, ok := a.saveDraft(w, r, s, d); ok {
		writeJSON(w, http.StatusOK, map[string]any{"uid": uid, "messageId": d.MessageID, "attachments": d.Attachments})
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
	d, ok := a.readDraft(w, r, s)
	if !ok {
		return
	}
	// Once the message has arrived, a closed tab must not stop it from being queued.
	r = r.WithContext(context.WithoutCancel(r.Context()))
	if len(d.Recipients()) == 0 {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	folder, uid, ok := a.saveDraft(w, r, s, d)
	if !ok {
		return
	}
	id, err := a.Outbox.Queue(r.Context(), a.account(r, s), folder, uid)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
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
