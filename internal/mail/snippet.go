package mail

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
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
			s = section(path, snippetFetch)
			sections[key] = s
		}
		wanted[m.UID] = want{part: part, section: s}
	}
	out := map[imap.UID]string{}
	if len(wanted) == 0 {
		return out, nil
	}

	// One FETCH per distinct section, so no message is asked for a part it does not have. They
	// run one after another: go-imap cannot say which of several pipelined FETCHes an untagged
	// answer belongs to.
	bySection := map[*imap.FetchItemBodySection]imap.UIDSet{}
	for uid, w := range wanted {
		set := bySection[w.section]
		set.AddNum(uid)
		bySection[w.section] = set
	}
	for s, uids := range bySection {
		bufs, err := c.Fetch(uids, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{s}}).Collect()
		if err != nil {
			return nil, err
		}
		for _, b := range bufs {
			out[b.UID] = snippet(b.FindBodySection(s), wanted[b.UID].part)
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
		if isAttachment(single) {
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
	htmlDrop = regexp.MustCompile(`(?is)<(style|script|head|title)\b.*?</(style|script|head|title)\s*>`)
	htmlTag  = regexp.MustCompile(`(?s)<[^>]*>`)
	// A style, script, head or title block that the fetch cut off before it closed.
	htmlUnclosed = regexp.MustCompile(`(?is)<(style|script|head|title)\b.*$`)
	// A tag the fetch cut off part-way.
	htmlCutTag  = regexp.MustCompile(`<[^>]*$`)
	quotedLines = regexp.MustCompile(`(?m)^\s*>.*$`)
)

// htmlText reduces the start of an HTML document to its visible text. The fetch may stop
// inside the head, a style block or a tag, so anything left open at the end is dropped too.
func htmlText(doc string) string {
	if i := strings.Index(strings.ToLower(doc), "<body"); i >= 0 {
		doc = doc[i:]
	}
	doc = htmlDrop.ReplaceAllString(doc, " ")
	doc = htmlUnclosed.ReplaceAllString(doc, " ")
	doc = htmlTag.ReplaceAllString(doc, " ")
	doc = htmlCutTag.ReplaceAllString(doc, "")
	return html.UnescapeString(doc)
}

// snippet turns the first bytes of a body part into a line of plain text.
func snippet(raw []byte, part *imap.BodyStructureSinglePart) string {
	// The fetch stops part-way, possibly inside a base64 quantum or a "=XX" escape: trim to
	// what decodes, then decode as a whole body would be.
	switch strings.ToLower(part.Encoding) {
	case "base64":
		raw = bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, raw)
		raw = raw[:len(raw)/4*4]
	case "quoted-printable":
		if i := bytes.LastIndexByte(raw, '='); i >= 0 && len(raw)-i < 3 {
			raw = raw[:i]
		}
	}
	decoded, _ := io.ReadAll(textDecoder(bytes.NewReader(raw), part)) // keep what decoded

	text := string(decoded)
	if strings.EqualFold(part.Subtype, "html") {
		text = htmlText(text)
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
