package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"mime/quotedprintable"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

const (
	snippetFetch = 2048 // bytes of the body part read for a snippet
	snippetRunes = 160  // longest snippet shown
)

// snippets returns the start of each message's text, keyed by UID. Messages without a text
// part get none.
func (c *Conn) snippets(msgs []*imapclient.FetchMessageBuffer) (map[imap.UID]string, error) {
	type want struct {
		part    *imap.BodyStructureSinglePart
		section *imap.FetchItemBodySection
	}
	wanted := map[imap.UID]want{}
	sections := map[string]*imap.FetchItemBodySection{}
	for _, m := range msgs {
		path, part := textPart(m.BodyStructure)
		if part == nil {
			continue
		}
		// One section per part path, shared by every message whose text sits at that path.
		key := fmt.Sprint(path)
		s := sections[key]
		if s == nil {
			s = &imap.FetchItemBodySection{Part: path, Peek: true, Partial: &imap.SectionPartial{Size: snippetFetch}}
			if len(path) == 0 {
				s.Specifier = imap.PartSpecifierText
			}
			sections[key] = s
		}
		wanted[m.UID] = want{part: part, section: s}
	}
	out := map[imap.UID]string{}
	if len(wanted) == 0 {
		return out, nil
	}

	// One FETCH per distinct section, so no message is sent parts it does not have.
	for _, s := range sections {
		var uids imap.UIDSet
		for uid, w := range wanted {
			if w.section == s {
				uids.AddNum(uid)
			}
		}
		bufs, err := c.Fetch(uids, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{s}}).Collect()
		if err != nil {
			return nil, err
		}
		for _, b := range bufs {
			w := wanted[b.UID]
			out[b.UID] = snippet(b.FindBodySection(s), w.part)
		}
	}
	return out, nil
}

// textPart picks the part a person would read first: the first text/plain part that is not an
// attachment, else the first such text/html part. path is nil for a single-part message.
func textPart(bs imap.BodyStructure) (path []int, part *imap.BodyStructureSinglePart) {
	if bs == nil {
		return nil, nil
	}
	var htmlPath []int
	var htmlPart *imap.BodyStructureSinglePart
	bs.Walk(func(p []int, s imap.BodyStructure) bool {
		single, ok := s.(*imap.BodyStructureSinglePart)
		if !ok || part != nil {
			return part == nil
		}
		if d := single.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
			return true
		}
		switch strings.ToLower(single.MediaType()) {
		case "text/plain":
			path, part = slices.Clone(p), single
		case "text/html":
			if htmlPart == nil {
				htmlPath, htmlPart = slices.Clone(p), single
			}
		}
		return part == nil
	})
	if part == nil {
		path, part = htmlPath, htmlPart
	}
	// A single-part message is fetched as BODY[TEXT], not BODY[1].
	if _, multi := bs.(*imap.BodyStructureMultiPart); !multi {
		path = nil
	}
	return path, part
}

var (
	htmlDrop    = regexp.MustCompile(`(?is)<(style|script|head|title)\b.*?</(style|script|head|title)\s*>`)
	htmlTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	quotedLines = regexp.MustCompile(`(?m)^\s*>.*$`)
)

// snippet turns the first bytes of a body part into a line of plain text.
func snippet(raw []byte, part *imap.BodyStructureSinglePart) string {
	var r io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(part.Encoding) {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, raw)
		clean = clean[:len(clean)/4*4] // the fetch may end mid-quantum
		r = base64.NewDecoder(base64.StdEncoding, bytes.NewReader(clean))
	case "quoted-printable":
		// The fetch may end inside an "=XX" escape.
		if i := bytes.LastIndexByte(raw, '='); i >= 0 && len(raw)-i < 3 {
			raw = raw[:i]
		}
		r = quotedprintable.NewReader(bytes.NewReader(raw))
	}
	if cs := part.Params["charset"]; cs != "" && !strings.EqualFold(cs, "utf-8") && !strings.EqualFold(cs, "us-ascii") {
		if converted, err := charset.Reader(cs, r); err == nil {
			r = converted
		}
	}
	decoded, _ := io.ReadAll(r) // a truncated tail is expected; keep what decoded

	text := string(decoded)
	if strings.EqualFold(part.Subtype, "html") {
		text = htmlDrop.ReplaceAllString(text, " ")
		text = htmlTag.ReplaceAllString(text, " ")
		text = html.UnescapeString(text)
	}
	text = strings.ToValidUTF8(text, "")
	text = quotedLines.ReplaceAllString(text, "")
	// Fields splits on every Unicode space, the non-breaking one included.
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) > snippetRunes {
		runes := []rune(text)
		text = strings.TrimSpace(string(runes[:snippetRunes])) + "…"
	}
	return text
}
