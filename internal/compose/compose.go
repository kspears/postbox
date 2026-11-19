// Package compose turns what the composer holds into a message, and a saved draft back into
// what the composer holds.
package compose

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"html"
	"io"
	"mime"
	"regexp"
	"strconv"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/kspears/postbox/internal/render"
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

// NewDraft is an empty draft from me, with every list empty rather than nil.
func NewDraft(me Person) *Draft {
	return &Draft{From: me, To: []Person{}, Cc: []Person{}, Bcc: []Person{}, Attachments: []Attachment{}}
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
	domain := "postbox.invalid"
	if _, d, ok := strings.Cut(from, "@"); ok && d != "" {
		domain = d
	}
	return strings.ToLower(rand.Text()) + "@" + domain
}

// Build writes d as a message, Bcc header included, as it is kept in Drafts and Sent (the
// outbox removes it from the recipients' copy). data supplies the content of attachments that
// refer to parts on the server.
func Build(d *Draft, date time.Time, data func(Attachment) ([]byte, error)) ([]byte, error) {
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
	set("Bcc", d.Bcc)
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
	// A rich-text message is its HTML, with the text made from it; a plain one is its text,
	// with the HTML made from it.
	body, text := render.Composed(d.HTML), d.Text
	if body == "" {
		body = TextToHTML(d.Text)
	} else {
		text = HTMLToText(body)
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
		// Header values come from other people's mail; a line break would end the header early.
		a.Name, a.Type, a.ContentID = oneLine(a.Name), oneLine(a.Type), oneLine(a.ContentID)
		content := a.Data
		if content == nil {
			if content, err = data(a); err != nil {
				return nil, fmt.Errorf("attachment %q: %w", a.Name, err)
			}
		}
		pw, err := createPart(w, a)
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

// Saved is where d's attachments are once Build's message is stored at folder/uid: Build writes
// the text and HTML as part 1 and the attachments after it, in order, as parts 2, 3 and on.
func Saved(d *Draft, folder string, uid uint32) []Attachment {
	out := make([]Attachment, len(d.Attachments))
	for i, a := range d.Attachments {
		if a.Data != nil {
			a.Size = int64(len(a.Data))
		}
		a.Folder, a.UID, a.Part, a.Data = folder, uid, strconv.Itoa(i+2), nil
		out[i] = a
	}
	return out
}

// createPart starts a's part: an image shown in the body (inline, with its Content-ID), or a
// file attachment. Both sit beside the text, so a's part number does not depend on which.
func createPart(w *gomail.Writer, a Attachment) (io.WriteCloser, error) {
	if a.Inline && a.ContentID != "" {
		t, params, err := mime.ParseMediaType(a.Type)
		if err != nil {
			t, params = "application/octet-stream", map[string]string{}
		}
		params["name"] = a.Name
		var h gomail.InlineHeader
		h.Set("Content-Type", mime.FormatMediaType(t, params))
		h.Set("Content-Transfer-Encoding", "base64")
		h.Set("Content-ID", "<"+a.ContentID+">")
		return w.CreateSingleInline(h)
	}
	var h gomail.AttachmentHeader
	h.Set("Content-Type", mediaType(a.Type))
	h.SetFilename(a.Name)
	return w.CreateAttachment(h)
}

var lineBreaks = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

func oneLine(s string) string { return lineBreaks.Replace(s) }

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

// HTMLToText is a readable plain-text version of an HTML body: one line per paragraph, quoted
// passages marked "> ", list items "- " or "1. ", and a link's address after its text when the
// two differ.
func HTMLToText(doc string) string {
	root, err := nethtml.Parse(strings.NewReader(doc))
	if err != nil {
		return ""
	}
	var w textWriter
	w.walk(root)
	w.endLine()
	lines := strings.Split(strings.Join(w.lines, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

var blankRuns = regexp.MustCompile(`\n{3,}`)

// textWriter collects lines of text, each prefixed by the quote depth it was written at.
type textWriter struct {
	lines  []string
	line   strings.Builder
	quote  int
	open   bool   // the current line has text
	marker string // a list marker, written before the next text
}

func (w *textWriter) write(s string) {
	if strings.TrimSpace(s) == "" && !w.open {
		return
	}
	w.line.WriteString(w.marker + s)
	w.marker = ""
	w.open = true
}

func (w *textWriter) endLine() {
	prefix := strings.Repeat("> ", w.quote)
	if w.open {
		w.lines = append(w.lines, prefix+strings.TrimSpace(w.line.String()))
	} else if w.line.Len() == 0 && len(w.lines) > 0 && w.lines[len(w.lines)-1] != strings.TrimSpace(prefix) {
		w.lines = append(w.lines, strings.TrimSpace(prefix))
	}
	w.line.Reset()
	w.open = false
}

var spaces = regexp.MustCompile(`[ \t\r\n]+`)

func (w *textWriter) walk(n *nethtml.Node) {
	switch n.Type {
	case nethtml.TextNode:
		w.write(spaces.ReplaceAllString(n.Data, " "))
		return
	case nethtml.ElementNode:
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Head, atom.Title:
			return
		case atom.Br:
			w.flush()
			return
		case atom.Blockquote:
			w.flush()
			w.quote++
			w.children(n)
			w.flush()
			w.quote--
			return
		case atom.Li:
			w.flush()
			marker := "- "
			if n.Parent != nil && n.Parent.DataAtom == atom.Ol {
				marker = strconv.Itoa(1+countBefore(n)) + ". "
			}
			w.marker = marker
			w.children(n)
			w.marker = ""
			w.flush()
			return
		case atom.A:
			before := w.line.Len()
			w.children(n)
			label := strings.TrimSpace(w.line.String()[before:])
			if href := attr(n, "href"); href != "" && !strings.HasPrefix(href, "mailto:") && href != label {
				w.write(" <" + href + ">")
			}
			return
		case atom.P:
			// One paragraph is one line, as the composer makes one per press of Enter; an empty
			// one is a blank line.
			w.flush()
			w.children(n)
			w.endLine()
			return
		case atom.Div, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Pre, atom.Tr, atom.Ul, atom.Ol, atom.Hr:
			w.flush()
			w.children(n)
			w.flush()
			return
		}
	}
	w.children(n)
}

// flush ends the current line if it has text.
func (w *textWriter) flush() {
	if w.open {
		w.endLine()
	}
}

func (w *textWriter) children(n *nethtml.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}
}

func countBefore(n *nethtml.Node) int {
	count := 0
	for s := n.PrevSibling; s != nil; s = s.PrevSibling {
		if s.Type == nethtml.ElementNode && s.DataAtom == atom.Li {
			count++
		}
	}
	return count
}

func attr(n *nethtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// Address formats a person for reading, as in a quote header: Name <address>.
func Address(p Person) string {
	if p.Name == "" {
		return p.Email
	}
	return p.Name + " <" + p.Email + ">"
}
