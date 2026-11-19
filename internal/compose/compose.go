// Package compose turns what the composer holds into a message, and a saved draft back into
// what the composer holds.
package compose

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"mime"
	"regexp"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
)

// Person is a name and address.
type Person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (p Person) address() *gomail.Address { return &gomail.Address{Name: p.Name, Address: p.Email} }

// Attachment is a file carried by the message: a part of a message already on the server (the
// one being forwarded, or the draft itself), or a file the person added.
type Attachment struct {
	Folder string `json:"folder,omitempty"`
	UID    uint32 `json:"uid,omitempty"`
	Part   string `json:"part,omitempty"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	// Data is the file's content, when it came with the request rather than from the server.
	Data []byte `json:"data,omitempty"`
	// Inline marks an image shown in the body, referred to there as cid:<ContentID>.
	Inline    bool   `json:"inline,omitempty"`
	ContentID string `json:"contentId,omitempty"`
}

// Draft is the composer's content.
type Draft struct {
	// UID is the draft's place in the Drafts folder, 0 until it is first saved.
	UID         uint32       `json:"uid,omitempty"`
	From        Person       `json:"from"`
	To          []Person     `json:"to"`
	Cc          []Person     `json:"cc"`
	Bcc         []Person     `json:"bcc"`
	Subject     string       `json:"subject"`
	Text        string       `json:"text"`
	HTML        string       `json:"html,omitempty"`
	MessageID   string       `json:"messageId,omitempty"`
	InReplyTo   string       `json:"inReplyTo,omitempty"`
	References  []string     `json:"references,omitempty"`
	Attachments []Attachment `json:"attachments"`
}

// Recipients is everyone the message goes to, Bcc included, each address once.
func (d *Draft) Recipients() []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range [][]Person{d.To, d.Cc, d.Bcc} {
		for _, p := range list {
			addr := strings.ToLower(strings.TrimSpace(p.Email))
			if addr != "" && !seen[addr] {
				seen[addr] = true
				out = append(out, p.Email)
			}
		}
	}
	return out
}

// NewMessageID returns a fresh Message-ID (without angle brackets) under the sender's domain.
func NewMessageID(from string) string {
	b := make([]byte, 16)
	rand.Read(b)
	domain := "postbox.invalid"
	if _, d, ok := strings.Cut(from, "@"); ok && d != "" {
		domain = d
	}
	return hex.EncodeToString(b) + "@" + domain
}

// Build writes d as a message. With bcc the Bcc header is kept, as it is in Drafts and Sent;
// without it the header is left out, as it is for the copy the recipients get. data supplies
// the content of attachments that refer to parts on the server.
func Build(d *Draft, date time.Time, bcc bool, data func(Attachment) ([]byte, error)) ([]byte, error) {
	var h gomail.Header
	h.SetDate(date)
	h.SetAddressList("From", []*gomail.Address{d.From.address()})
	set := func(key string, people []Person) {
		if len(people) == 0 {
			return
		}
		addrs := make([]*gomail.Address, len(people))
		for i, p := range people {
			addrs[i] = p.address()
		}
		h.SetAddressList(key, addrs)
	}
	set("To", d.To)
	set("Cc", d.Cc)
	if bcc {
		set("Bcc", d.Bcc)
	}
	h.SetSubject(d.Subject)
	if d.MessageID == "" {
		d.MessageID = NewMessageID(d.From.Email)
	}
	h.SetMessageID(d.MessageID)
	if d.InReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{d.InReplyTo})
	}
	if len(d.References) > 0 {
		h.SetMsgIDList("References", d.References)
	}
	h.Set("MIME-Version", "1.0")
	h.Set("User-Agent", "Postbox")

	var buf bytes.Buffer
	w, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	body := d.HTML
	if body == "" {
		body = TextToHTML(d.Text)
	}
	text := d.Text
	if text == "" && d.HTML != "" {
		text = HTMLToText(d.HTML)
	}

	// Text and HTML side by side; images shown in the body travel with the HTML.
	inline, err := w.CreateInline()
	if err != nil {
		return nil, err
	}
	if err := writeInline(inline, "text/plain", text); err != nil {
		return nil, err
	}
	if err := writeInline(inline, "text/html", "<!doctype html><html><body>"+body+"</body></html>"); err != nil {
		return nil, err
	}
	if err := inline.Close(); err != nil {
		return nil, err
	}

	for _, a := range d.Attachments {
		content := a.Data
		if content == nil {
			if content, err = data(a); err != nil {
				return nil, fmt.Errorf("attachment %q: %w", a.Name, err)
			}
		}
		var ah gomail.AttachmentHeader
		ah.Set("Content-Type", mediaType(a.Type))
		ah.SetFilename(a.Name)
		if a.Inline && a.ContentID != "" {
			ah.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": a.Name}))
			ah.Set("Content-ID", "<"+a.ContentID+">")
		}
		pw, err := w.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write(content); err != nil {
			return nil, err
		}
		if err := pw.Close(); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeInline(w *gomail.InlineWriter, mediaType, content string) error {
	var h gomail.InlineHeader
	h.Set("Content-Type", mediaType+"; charset=utf-8")
	h.Set("Content-Transfer-Encoding", "quoted-printable")
	pw, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(pw, content); err != nil {
		return err
	}
	return pw.Close()
}

func mediaType(t string) string {
	if _, _, err := mime.ParseMediaType(t); err != nil || t == "" {
		return "application/octet-stream"
	}
	return t
}

var webAddress = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// TextToHTML is the HTML version of plain text: escaped, line breaks kept, web addresses linked,
// and quoted lines ("> ") shown as quotes.
func TextToHTML(text string) string {
	var b strings.Builder
	quoting := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		quoted := strings.HasPrefix(line, ">")
		if quoted && !quoting {
			b.WriteString(`<blockquote style="margin:0 0 0 .8ex;border-left:1px solid #ccc;padding-left:1ex">`)
		}
		if !quoted && quoting {
			b.WriteString("</blockquote>")
		}
		quoting = quoted
		if quoted {
			line = strings.TrimPrefix(strings.TrimPrefix(line, ">"), " ")
		}
		last := 0
		for _, m := range webAddress.FindAllStringIndex(line, -1) {
			b.WriteString(html.EscapeString(line[last:m[0]]))
			u := html.EscapeString(line[m[0]:m[1]])
			b.WriteString(`<a href="` + u + `">` + u + `</a>`)
			last = m[1]
		}
		b.WriteString(html.EscapeString(line[last:]))
		b.WriteString("<br>")
	}
	if quoting {
		b.WriteString("</blockquote>")
	}
	return b.String()
}

var (
	htmlBreak = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|li|h[1-6]|tr|blockquote)>`)
	htmlSkip  = regexp.MustCompile(`(?is)<(style|script|head|title)\b.*?</(style|script|head|title)\s*>`)
	htmlTag   = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRuns = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText is a readable plain-text version of an HTML body.
func HTMLToText(doc string) string {
	doc = htmlSkip.ReplaceAllString(doc, "")
	doc = htmlBreak.ReplaceAllString(doc, "\n")
	doc = htmlTag.ReplaceAllString(doc, "")
	doc = html.UnescapeString(doc)
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// Address formats a person for reading, as in a quote header: Name <address>.
func Address(p Person) string {
	if p.Name == "" {
		return p.Email
	}
	return p.Name + " <" + p.Email + ">"
}
