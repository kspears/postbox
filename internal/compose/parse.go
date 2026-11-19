package compose

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"strconv"
	"strings"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	gomail "github.com/emersion/go-message/mail"
)

// Original is a received message as reply and forward need it.
type Original struct {
	From, ReplyTo, To, Cc []Person
	Subject               string
	Date                  string
	MessageID             string
	References            []string
	Text                  string // the readable text, from the plain part or the HTML one
	HTML                  string
	// Attachments are the parts a forward carries, by part number.
	Attachments []Attachment
}

// Parse reads a message as reply, forward and an opened draft need it. folder and uid say where it
// is, so its attachments can refer to their parts there.
func Parse(raw []byte, folder string, uid uint32) (*Original, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, err
	}
	h := gomail.Header{Header: entity.Header}
	o := &Original{
		From:    people(h, "From"),
		ReplyTo: people(h, "Reply-To"),
		To:      people(h, "To"),
		Cc:      people(h, "Cc"),
	}
	o.Subject, _ = h.Subject()
	o.Date = h.Get("Date")
	o.MessageID, _ = h.MessageID()
	o.References, _ = h.MsgIDList("References")
	if len(o.References) == 0 {
		o.References, _ = h.MsgIDList("In-Reply-To")
	}

	err = entity.Walk(func(path []int, part *message.Entity, err error) error {
		if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
			return nil // a broken part is skipped, not fatal
		}
		mediaType, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if strings.HasPrefix(mediaType, "multipart/") {
			return nil
		}
		if mediaType == "" {
			mediaType = "text/plain"
		}
		disposition, dparams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		name := dparams["filename"]
		if name == "" {
			name = params["name"]
		}
		attached := disposition == "attachment" || (name != "" && !strings.HasPrefix(mediaType, "text/"))
		body, _ := io.ReadAll(io.LimitReader(part.Body, 8<<20))
		if strings.HasPrefix(mediaType, "text/") {
			body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
		}
		switch {
		case !attached && mediaType == "text/plain" && o.Text == "":
			o.Text = string(body)
		case !attached && mediaType == "text/html" && o.HTML == "":
			o.HTML = string(body)
		case mediaType == "message/rfc822" || attached || len(path) > 0:
			if name == "" {
				name = "attachment"
			}
			a := Attachment{Folder: folder, UID: uid, Part: partName(path), Name: name, Type: mediaType, Size: int64(len(body))}
			if id := strings.Trim(part.Header.Get("Content-ID"), "<> "); id != "" && disposition != "attachment" {
				a.Inline, a.ContentID = true, id
			}
			o.Attachments = append(o.Attachments, a)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if o.Text == "" && o.HTML != "" {
		o.Text = HTMLToText(o.HTML)
	}
	return o, nil
}

func partName(path []int) string {
	parts := make([]string, len(path))
	for i, n := range path {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

func people(h gomail.Header, key string) []Person {
	addrs, err := h.AddressList(key)
	if err != nil {
		return []Person{}
	}
	out := make([]Person, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, Person{Name: a.Name, Email: a.Address})
	}
	return out
}

// FromDraft turns a saved draft back into the composer's content.
func FromDraft(raw []byte, folder string, uid uint32) (*Draft, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, err
	}
	o, err := Parse(raw, folder, uid)
	if err != nil {
		return nil, err
	}
	h := gomail.Header{Header: entity.Header}
	d := &Draft{
		UID: uid, To: o.To, Cc: o.Cc, Bcc: people(h, "Bcc"), Subject: o.Subject, Text: o.Text,
		MessageID: o.MessageID, Attachments: o.Attachments,
	}
	if d.Attachments == nil {
		d.Attachments = []Attachment{}
	}
	if len(o.From) > 0 {
		d.From = o.From[0]
	}
	if ids, _ := h.MsgIDList("In-Reply-To"); len(ids) > 0 {
		d.InReplyTo = ids[0]
	}
	d.References, _ = h.MsgIDList("References")
	// The HTML Postbox writes for a plain draft is made from its text; only keep a body that
	// says more than that.
	if body := innerBody(o.HTML); body != "" && body != TextToHTML(o.Text) {
		d.HTML = body
	}
	return d, nil
}

// innerBody is what is between <body> and </body>, or the whole when there is no body element.
func innerBody(doc string) string {
	lower := strings.ToLower(doc)
	start := strings.Index(lower, "<body>")
	end := strings.LastIndex(lower, "</body>")
	if start < 0 || end < start {
		return doc
	}
	return doc[start+len("<body>") : end]
}

// ErrNoRecipients means a message to send names nobody to send it to.
var ErrNoRecipients = errors.New("no recipients")
