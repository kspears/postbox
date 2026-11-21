package api

import (
	"encoding/json"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"

	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/render"
	"github.com/kspears/postbox/internal/session"
)

const errNotFound = "not_found"

// messageView is a message as the reading pane receives it.
type messageView struct {
	mail.Message
	// Body is where the frame loads the rendered body from; empty when there is none.
	Body string `json:"body,omitempty"`
	// Sender is the From address that "Always load from this sender" applies to.
	Sender        string `json:"sender,omitempty"`
	ImagesAllowed bool   `json:"imagesAllowed"`
}

func bodyURL(folder string, uid imap.UID, images bool) string {
	q := url.Values{"folder": {folder}, "uid": {strconv.FormatUint(uint64(uid), 10)}}
	if images {
		q.Set("images", "1")
	}
	return "/api/body?" + q.Encode()
}

func uidParam(r *http.Request, name string) (imap.UID, bool) {
	n, err := strconv.ParseUint(r.URL.Query().Get(name), 10, 32)
	return imap.UID(n), err == nil && n > 0
}

// conversation answers GET /api/conversation?folder=<name>&id=<uid>: the messages of the
// conversation holding that message, with the account's replies from Sent, oldest first.
func (a *apiHandler) conversation(w http.ResponseWriter, r *http.Request, s *session.Session) {
	folder := r.URL.Query().Get("folder")
	id, ok := uidParam(r, "id")
	if folder == "" || !ok {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var msgs []mail.Message
	if !a.withMail(w, r, s, func(c *mail.Conn) error {
		sent, err := c.SentFolder()
		if err != nil {
			return err
		}
		msgs, err = c.Conversation(folder, id, sent)
		return err
	}) {
		return
	}

	var senders []string
	for _, m := range msgs {
		if len(m.From) > 0 && !slices.Contains(senders, m.From[0].Email) {
			senders = append(senders, m.From[0].Email)
		}
	}
	allowed, err := a.Store.ImagesAllowed(r.Context(), s.Account, senders)
	if err != nil {
		internalError(w, r, err)
		return
	}
	views := make([]messageView, 0, len(msgs))
	for _, m := range msgs {
		v := messageView{Message: m}
		if len(m.From) > 0 {
			v.Sender = m.From[0].Email
			v.ImagesAllowed = allowed[v.Sender]
		}
		if m.HasBody {
			v.Body = bodyURL(m.Folder, m.UID, v.ImagesAllowed)
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": views})
}

// The frame document a body is shown in. It holds no script; the app reads the remote-image
// count from the meta element, which the frame's shared origin allows.
var bodyPage = template.Must(template.New("body").Parse(`<!doctype html>
<html><head><meta charset="utf-8">
<meta name="referrer" content="no-referrer">
<meta name="postbox-remote-images" content="{{.RemoteImages}}">
<style>
html{color-scheme:light}
body{margin:0;padding:16px 4px;font:15px/1.5 system-ui,sans-serif;color:#1b1c1e;background:#fff;overflow-wrap:anywhere}
img{max-width:100%;height:auto}
table{max-width:100%}
.postbox-plain{white-space:pre-wrap;font:inherit}
</style>
<style>{{.CSS}}</style>
<style>html,body{height:auto!important;min-height:0!important;max-height:none!important;position:static!important}</style>
</head><body>{{.HTML}}</body></html>
`))

// fromAppFrame reports whether r may ask for remote images: it carries no Fetch Metadata (an
// older browser) or comes from the app's own iframe. A link in another message opens the URL as
// a top-level navigation, which must not load remote images the person did not ask for.
func fromAppFrame(r *http.Request) bool {
	dest := r.Header.Get("Sec-Fetch-Dest")
	return dest == "" || (dest == "iframe" && r.Header.Get("Sec-Fetch-Site") == "same-origin")
}

// body answers GET /api/body?folder=<name>&uid=<uid>[&images=1] with the rendered body as a
// document of its own, for the reading pane's frame.
func (a *apiHandler) body(w http.ResponseWriter, r *http.Request, s *session.Session) {
	folder := r.URL.Query().Get("folder")
	uid, ok := uidParam(r, "uid")
	if folder == "" || !ok {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	images := r.URL.Query().Get("images") == "1" && fromAppFrame(r)
	// An error answer is shown in the reading pane's frame too, which must be allowed to hold it.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'; sandbox allow-same-origin")
	var res render.Result
	if !a.withMail(w, r, s, func(c *mail.Conn) (err error) {
		res, err = c.Body(folder, uid, images)
		return err
	}) {
		return
	}

	// The guarantee behind "no request while images are blocked": even if the sanitiser let
	// something through, this policy stops the browser from fetching it.
	imgSrc := "data:"
	if images {
		imgSrc += " http: https:"
	}
	h := w.Header()
	// sandbox repeats the frame's own: opened on its own (from a link in another message) the
	// body still runs nothing. allow-same-origin keeps it readable by the app's frame code.
	h.Set("Content-Security-Policy", "default-src 'none'; img-src "+imgSrc+"; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'; frame-ancestors 'self'; sandbox allow-same-origin allow-popups allow-popups-to-escape-sandbox")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-DNS-Prefetch-Control", "off")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Type", "text/html; charset=utf-8")
	bodyPage.Execute(w, map[string]any{
		"RemoteImages": res.RemoteImages,
		"CSS":          template.CSS(res.CSS),   // filtered by render.FilterStyleSheet
		"HTML":         template.HTML(res.HTML), // sanitised by render.HTML or built by render.Text
	})
}

// attachment answers GET /api/attachment?folder=<name>&uid=<uid>&part=<n.n> with the decoded
// part as a download. It is never shown inline, whatever type it claims.
func (a *apiHandler) attachment(w http.ResponseWriter, r *http.Request, s *session.Session) {
	q := r.URL.Query()
	folder, part := q.Get("folder"), q.Get("part")
	uid, ok := uidParam(r, "uid")
	if folder == "" || !ok || !validPart(part) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	started := false
	a.withStream(w, r, s, func(c *mail.Conn) error {
		err := c.Attachment(folder, uid, part, func(att mail.Attachment) {
			started = true
			h := w.Header()
			h.Set("Content-Type", "application/octet-stream")
			h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": att.Name}))
			h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
			h.Set("Cache-Control", "private, no-store")
		}, w)
		if started && err != nil {
			// Headers are gone already; the browser sees a cut-off download.
			return nil
		}
		return err
	})
}

func validPart(p string) bool {
	if p == "" {
		return false
	}
	for _, n := range strings.Split(p, ".") {
		if v, err := strconv.Atoi(n); err != nil || v < 1 {
			return false
		}
	}
	return true
}

// markRead answers POST /api/seen {"folder": ..., "uids": [...]}.
func (a *apiHandler) markRead(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Folder string     `json:"folder"`
		UIDs   []imap.UID `json:"uids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || req.Folder == "" || len(req.UIDs) == 0 {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if a.withMail(w, r, s, func(c *mail.Conn) error { return c.MarkRead(req.Folder, req.UIDs) }) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// allowImages answers POST /api/images/allow {"sender": ...}: "Always load from this sender".
func (a *apiHandler) allowImages(w http.ResponseWriter, r *http.Request, s *session.Session) {
	var req struct {
		Sender string `json:"sender"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || !strings.Contains(req.Sender, "@") {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if err := a.Store.AllowImages(r.Context(), s.Account, strings.ToLower(strings.TrimSpace(req.Sender))); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
