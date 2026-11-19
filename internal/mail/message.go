package mail

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"

	"github.com/kspears/postbox/internal/render"
)

// ErrNotFound means the message or conversation asked for is not in the folder (any more).
var ErrNotFound = errors.New("not found")

// Limits on what one message view reads from the server.
const (
	maxBodyBytes   = 4 << 20  // the HTML or text part shown
	maxInlineBytes = 10 << 20 // all embedded images together
)

// Attachment is a part offered for download.
type Attachment struct {
	Part string `json:"part"` // IMAP part number, such as "2" or "1.3"
	Name string `json:"name"`
	Type string `json:"type"`
	Size uint32 `json:"size"` // encoded size, a little larger than the file
}

// Message is one message of an open conversation, without its body.
type Message struct {
	Folder      string       `json:"folder"`
	UID         imap.UID     `json:"uid"`
	From        []Person     `json:"from"`
	To          []Person     `json:"to"`
	Cc          []Person     `json:"cc"`
	Date        time.Time    `json:"date"`
	Subject     string       `json:"subject"`
	Unread      bool         `json:"unread"`
	Flagged     bool         `json:"flagged"`
	HasBody     bool         `json:"hasBody"`
	Attachments []Attachment `json:"attachments"`
	messageID   string
}

// layout is what a message's structure says to show.
type layout struct {
	body        []int // path of the part to display; nil with bodyPart set means BODY[TEXT]
	bodyPart    *imap.BodyStructureSinglePart
	inline      map[string]partAt // embedded images by Content-ID
	attachments []Attachment
}

type partAt struct {
	path []int
	part *imap.BodyStructureSinglePart
}

var bitmap = map[string]bool{"image/png": true, "image/gif": true, "image/jpeg": true, "image/jpg": true, "image/webp": true, "image/bmp": true}

func plan(bs imap.BodyStructure) layout {
	l := layout{inline: map[string]partAt{}, attachments: []Attachment{}}
	if bs == nil {
		return l
	}
	_, multi := bs.(*imap.BodyStructureMultiPart)
	var text, html *partAt
	var texts []partAt // every text part not marked as an attachment
	images := map[string]partAt{}
	bs.Walk(func(path []int, s imap.BodyStructure) bool {
		single, ok := s.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		p := partAt{path: slices.Clone(path), part: single}
		mediaType := strings.ToLower(single.MediaType())
		id := strings.Trim(single.ID, "<> ")
		switch {
		case single.MessageRFC822 != nil:
			l.attachments = append(l.attachments, attachmentOf(p, "message.eml"))
			return false // its own parts belong to it, not to this message
		case isAttachment(single):
			l.attachments = append(l.attachments, attachmentOf(p, "attachment"))
		case id != "" && bitmap[mediaType]:
			images[id] = p
		case mediaType == "text/html" || mediaType == "text/plain":
			texts = append(texts, p)
			if mediaType == "text/html" && html == nil {
				html = &texts[len(texts)-1]
			}
			if mediaType == "text/plain" && text == nil {
				text = &texts[len(texts)-1]
			}
		default:
			// Calendars, CSV files and anything else not meant as the message itself.
			l.attachments = append(l.attachments, attachmentOf(p, "attachment"))
		}
		return true
	})
	chosen := cmp.Or(html, text)
	if chosen != nil {
		l.body, l.bodyPart = chosen.path, chosen.part
		if !multi {
			l.body = nil
		}
	}
	// Nothing in the message may be neither shown nor offered: the other text parts (beyond
	// the plain version of the HTML one) and, when the body is plain text and cannot show them,
	// the embedded images are listed as attachments.
	for _, t := range texts {
		alternative := html != nil && text != nil && t.part == text.part
		if (chosen == nil || t.part != chosen.part) && !alternative {
			l.attachments = append(l.attachments, attachmentOf(t, "text.txt"))
		}
	}
	for id, p := range images {
		if html != nil {
			l.inline[id] = p
		} else {
			l.attachments = append(l.attachments, attachmentOf(p, "image"))
		}
	}
	slices.SortFunc(l.attachments, func(a, b Attachment) int { return strings.Compare(a.Part, b.Part) })
	return l
}

func attachmentOf(p partAt, fallback string) Attachment {
	name := p.part.Filename()
	if name == "" {
		name = fallback
	}
	return Attachment{Part: partName(p.path), Name: name, Type: strings.ToLower(p.part.MediaType()), Size: p.part.Size}
}

func partName(path []int) string {
	parts := make([]string, len(path))
	for i, n := range path {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ".")
}

// section returns the BODY[] section for a part path; nil means the single-part message's text.
func section(path []int, size int64) *imap.FetchItemBodySection {
	s := &imap.FetchItemBodySection{Part: path, Peek: true, Partial: &imap.SectionPartial{Size: size}}
	if len(path) == 0 {
		s.Specifier = imap.PartSpecifierText
	}
	return s
}

// Conversation returns the messages of the conversation in folder that contains id, oldest
// first, joined by the account's own replies from sent (the Sent folder's name, or "").
func (c *Conn) Conversation(folder string, id imap.UID, sent string) ([]Message, error) {
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}
	threads, err := c.threads()
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(threads, func(t []imap.UID) bool { return slices.Contains(t, id) })
	if i < 0 {
		return nil, ErrNotFound
	}
	msgs, err := c.messages(folder, threads[i])
	if err != nil {
		return nil, err
	}

	if sent != "" && sent != folder && len(msgs) > 0 {
		replies, err := c.repliesIn(sent, msgs)
		if err != nil {
			return nil, err
		}
		for _, r := range replies {
			if !slices.ContainsFunc(msgs, func(m Message) bool { return m.messageID != "" && m.messageID == r.messageID }) {
				msgs = append(msgs, r)
			}
		}
	}
	slices.SortStableFunc(msgs, func(a, b Message) int { return a.Date.Compare(b.Date) })
	return msgs, nil
}

// messages fetches the headers and structure of uids in the open folder.
func (c *Conn) messages(folder string, uids []imap.UID) ([]Message, error) {
	bufs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
		UID: true, Flags: true, Envelope: true, InternalDate: true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}).Collect()
	if err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(bufs))
	for _, b := range bufs {
		l := plan(b.BodyStructure)
		m := Message{
			Folder: folder, UID: b.UID, Date: b.InternalDate,
			Unread:      !slices.Contains(b.Flags, imap.FlagSeen),
			Flagged:     slices.Contains(b.Flags, imap.FlagFlagged),
			HasBody:     l.bodyPart != nil,
			Attachments: l.attachments,
			From:        []Person{}, To: []Person{}, Cc: []Person{},
		}
		if e := b.Envelope; e != nil {
			m.From, m.To, m.Cc = people(e.From), people(e.To), people(e.Cc)
			m.Subject, m.messageID = e.Subject, e.MessageID
			if !e.Date.IsZero() {
				m.Date = e.Date
			}
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func people(addrs []imap.Address) []Person {
	out := make([]Person, 0, len(addrs))
	for _, a := range addrs {
		if a.IsGroupStart() || a.IsGroupEnd() {
			continue
		}
		out = append(out, Person{Name: a.Name, Email: strings.ToLower(a.Addr())})
	}
	return out
}

// repliesIn finds, in the Sent folder, messages that answer any message of the conversation.
func (c *Conn) repliesIn(sent string, msgs []Message) ([]Message, error) {
	var ids []string
	for _, m := range msgs {
		if m.messageID != "" {
			ids = append(ids, "<"+m.messageID+">")
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	// A reply names its parent in In-Reply-To and its ancestors in References. The newest
	// twenty are enough to catch every reply in practice and keep the search small.
	if len(ids) > 20 {
		ids = ids[len(ids)-20:]
	}
	var criteria *imap.SearchCriteria
	for _, id := range ids {
		for _, field := range []string{"In-Reply-To", "References"} {
			one := imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: field, Value: id}}}
			if criteria == nil {
				criteria = &one
				continue
			}
			criteria = &imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{*criteria, one}}}
		}
	}

	if _, err := c.Client.Select(sent, nil).Wait(); err != nil {
		if isClientNo(err) {
			return nil, nil // no Sent folder yet
		}
		return nil, err
	}
	found, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, err
	}
	uids := found.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	return c.messages(sent, uids)
}

// Body renders one message's body. It reports ErrNotFound when the message is gone.
func (c *Conn) Body(folder string, uid imap.UID, remoteImages bool) (render.Result, error) {
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return render.Result{}, err
	}
	bufs, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}).Collect()
	if err != nil {
		return render.Result{}, err
	}
	if len(bufs) == 0 {
		return render.Result{}, ErrNotFound
	}
	l := plan(bufs[0].BodyStructure)
	if l.bodyPart == nil {
		return render.Result{}, nil
	}

	body := section(l.body, maxBodyBytes)
	sections := []*imap.FetchItemBodySection{body}
	inline := map[string]*imap.FetchItemBodySection{}
	budget := int64(maxInlineBytes)
	for id, p := range l.inline {
		if int64(p.part.Size) > budget {
			continue
		}
		budget -= int64(p.part.Size)
		inline[id] = section(p.path, int64(p.part.Size)+1)
		sections = append(sections, inline[id])
	}
	bufs, err = c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, BodySection: sections}).Collect()
	if err != nil {
		return render.Result{}, err
	}
	if len(bufs) == 0 {
		return render.Result{}, ErrNotFound
	}

	opts := render.Options{RemoteImages: remoteImages, Inline: map[string]string{}}
	for id, s := range inline {
		p := l.inline[id].part
		data, err := io.ReadAll(decoder(bytes.NewReader(bufs[0].FindBodySection(s)), p.Encoding))
		if err == nil {
			opts.Inline[id] = "data:" + strings.ToLower(p.MediaType()) + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
	}
	text, _ := io.ReadAll(textDecoder(bytes.NewReader(bufs[0].FindBodySection(body)), l.bodyPart))
	if strings.EqualFold(l.bodyPart.Subtype, "html") {
		return render.HTML(strings.ToValidUTF8(string(text), "�"), opts), nil
	}
	return render.Text(strings.ToValidUTF8(string(text), "�")), nil
}

// decoder undoes a part's Content-Transfer-Encoding.
func decoder(r io.Reader, encoding string) io.Reader {
	switch strings.ToLower(encoding) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &stripSpace{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

// textDecoder undoes the transfer encoding, then converts the charset to UTF-8.
func textDecoder(r io.Reader, part *imap.BodyStructureSinglePart) io.Reader {
	r = decoder(r, part.Encoding)
	if cs := part.Params["charset"]; cs != "" && !strings.EqualFold(cs, "utf-8") && !strings.EqualFold(cs, "us-ascii") {
		if converted, err := charset.Reader(cs, r); err == nil {
			return converted
		}
	}
	return r
}

// stripSpace drops the line breaks and spaces base64 bodies are wrapped with.
type stripSpace struct{ r io.Reader }

func (s *stripSpace) Read(p []byte) (int, error) {
	for {
		n, err := s.r.Read(p)
		kept := p[:0]
		for _, c := range p[:n] {
			if c != '\r' && c != '\n' && c != ' ' && c != '\t' {
				kept = append(kept, c)
			}
		}
		if len(kept) > 0 || err != nil {
			return len(kept), err
		}
	}
}

// Attachment streams one decoded part of a message to w, after calling start with the part's
// details so the caller can send headers first.
func (c *Conn) Attachment(folder string, uid imap.UID, part string, start func(Attachment), w io.Writer) error {
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return err
	}
	bufs, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}).Collect()
	if err != nil {
		return err
	}
	if len(bufs) == 0 {
		return ErrNotFound
	}
	var found *partAt
	var att Attachment
	bufs[0].BodyStructure.Walk(func(path []int, s imap.BodyStructure) bool {
		single, ok := s.(*imap.BodyStructureSinglePart)
		if ok && partName(path) == part {
			found = &partAt{path: slices.Clone(path), part: single}
			att = attachmentOf(*found, "attachment")
		}
		return found == nil
	})
	if found == nil {
		return ErrNotFound
	}

	cmd := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{{Part: found.path, Peek: true}}})
	defer cmd.Close()
	msg := cmd.Next()
	if msg == nil {
		return cmp.Or(cmd.Close(), ErrNotFound)
	}
	for {
		item := msg.Next()
		if item == nil {
			return ErrNotFound
		}
		if data, ok := item.(imapclient.FetchItemDataBodySection); ok {
			start(att)
			if _, err := io.Copy(w, decoder(data.Literal, found.part.Encoding)); err != nil {
				return err
			}
			return cmd.Close()
		}
	}
}

// MarkRead sets \Seen on uids in folder.
func (c *Conn) MarkRead(folder string, uids []imap.UID) error {
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return err
	}
	return c.Store(imap.UIDSetNum(uids...), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close()
}
