package compose

import (
	"bytes"
	"errors"
	"mime"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message"
)

var when = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

func draft() *Draft {
	return &Draft{
		From:       Person{Name: "Alice", Email: "alice@example.test"},
		To:         []Person{{Name: "Bob", Email: "bob@example.test"}},
		Cc:         []Person{{Email: "carol@example.test"}},
		Bcc:        []Person{{Email: "dave@example.test"}},
		Subject:    "Café plans",
		Text:       "Hi Bob,\n\nSee https://example.org/plan.\n\n> earlier words\n",
		InReplyTo:  "orig@example.test",
		References: []string{"root@example.test", "orig@example.test"},
		Attachments: []Attachment{
			{Name: "notes.txt", Type: "text/plain", Data: []byte("bring the ladder")},
		},
	}
}

func TestBuildAndReadBack(t *testing.T) {
	raw, err := Build(draft(), when, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromDraft(raw, "Drafts", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Café plans" || got.From.Email != "alice@example.test" || len(got.To) != 1 || len(got.Cc) != 1 || len(got.Bcc) != 1 {
		t.Errorf("headers: %+v", got)
	}
	if strings.TrimRight(got.Text, "\r\n") != strings.TrimRight(draft().Text, "\n") {
		t.Errorf("text %q", got.Text)
	}
	if got.HTML != "" {
		t.Errorf("a plain draft came back with HTML of its own: %q", got.HTML)
	}
	if got.InReplyTo != "orig@example.test" || len(got.References) != 2 || got.MessageID == "" {
		t.Errorf("threading: %+v", got)
	}
	// Part 1 is the text and HTML; the attachment is IMAP part 2, where Saved says it is.
	if len(got.Attachments) != 1 || got.Attachments[0].Name != "notes.txt" || got.Attachments[0].Folder != "Drafts" || got.Attachments[0].UID != 7 || got.Attachments[0].Part != "2" {
		t.Errorf("attachments: %+v", got.Attachments)
	}
	if saved := Saved(draft(), "Drafts", 7); saved[0].Part != got.Attachments[0].Part || saved[0].Data != nil || saved[0].Size != 16 {
		t.Errorf("saved %+v, read back %+v", saved[0], got.Attachments[0])
	}
}

func TestRecipientsIncludeBcc(t *testing.T) {
	if got := draft().Recipients(); len(got) != 3 || got[2] != "dave@example.test" {
		t.Errorf("recipients %v", got)
	}
}

func TestBuildHasTextAndHTML(t *testing.T) {
	raw, _ := Build(draft(), when, nil)
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	entity.Walk(func(_ []int, p *message.Entity, _ error) error {
		mt, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		types = append(types, mt)
		return nil
	})
	want := "multipart/mixed multipart/alternative text/plain text/html text/plain"
	if strings.Join(types, " ") != want {
		t.Errorf("structure %q, want %q", strings.Join(types, " "), want)
	}
	o, err := Parse(raw, "Drafts", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o.HTML, "<blockquote") || !strings.Contains(o.HTML, `<a href="https://example.org/plan">`) {
		t.Errorf("the HTML part does not quote or link: %s", o.HTML)
	}
}

func TestBuildFetchesServerParts(t *testing.T) {
	d := draft()
	d.Attachments = []Attachment{{Folder: "INBOX", UID: 3, Part: "2", Name: "a.pdf", Type: "application/pdf"}}
	var asked Attachment
	_, err := Build(d, when, func(a Attachment) ([]byte, error) {
		asked = a
		return []byte("%PDF"), nil
	})
	if err != nil || asked.Part != "2" || asked.UID != 3 {
		t.Errorf("asked for %+v, %v", asked, err)
	}
	_, err = Build(d, when, func(Attachment) ([]byte, error) { return nil, errors.New("gone") })
	if err == nil {
		t.Error("a missing part was not reported")
	}
}

func TestParseOriginal(t *testing.T) {
	raw := []byte("From: Bob <bob@example.test>\r\nReply-To: lists@example.test\r\nTo: alice@example.test\r\nSubject: Re: Plans\r\nMessage-ID: <m2@example.test>\r\nReferences: <m1@example.test>\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Hello <b>there</b></p><p>Second</p>")
	o, err := Parse(raw, "INBOX", 9)
	if err != nil {
		t.Fatal(err)
	}
	if o.Text != "Hello there\nSecond" || o.ReplyTo[0].Email != "lists@example.test" || o.MessageID != "m2@example.test" || o.References[0] != "m1@example.test" {
		t.Errorf("got %+v", o)
	}
}

func TestRespond(t *testing.T) {
	me := Person{Email: "alice@example.test"}
	o := &Original{
		From:        []Person{{Name: "Bob", Email: "bob@example.test"}},
		To:          []Person{{Email: "Alice@example.test"}, {Email: "carol@example.test"}},
		Cc:          []Person{{Email: "dave@example.test"}, {Email: "carol@example.test"}},
		Subject:     "Plans",
		Date:        "Mon, 16 Nov 2026 09:00:00 +0000",
		MessageID:   "m2@example.test",
		References:  []string{"m1@example.test"},
		Text:        "Shall we?\n> earlier",
		Attachments: []Attachment{{Folder: "INBOX", UID: 4, Part: "2", Name: "map.pdf"}},
	}
	r := Respond(o, ModeReply, []Person{me})
	if r.Subject != "Re: Plans" || len(r.To) != 1 || r.To[0].Email != "bob@example.test" || len(r.Cc) != 0 || len(r.Attachments) != 0 {
		t.Errorf("reply %+v", r)
	}
	if r.InReplyTo != "m2@example.test" || strings.Join(r.References, " ") != "m1@example.test m2@example.test" {
		t.Errorf("threading %+v", r)
	}
	if !strings.Contains(r.Text, "> Shall we?\n>> earlier\n") || !strings.Contains(r.Text, "Bob <bob@example.test> wrote:") {
		t.Errorf("quote %q", r.Text)
	}

	all := Respond(o, ModeReplyAll, []Person{me})
	var to, cc []string
	for _, p := range all.To {
		to = append(to, p.Email)
	}
	for _, p := range all.Cc {
		cc = append(cc, p.Email)
	}
	if strings.Join(to, " ") != "bob@example.test carol@example.test" || strings.Join(cc, " ") != "dave@example.test" {
		t.Errorf("reply all to %v cc %v", to, cc)
	}

	f := Respond(o, ModeForward, []Person{me})
	if f.Subject != "Fwd: Plans" || len(f.To) != 0 || len(f.Attachments) != 1 || !strings.Contains(f.Text, "---------- Forwarded message ----------") {
		t.Errorf("forward %+v", f)
	}
	if Respond(&Original{Subject: "re: x", From: []Person{me}, To: []Person{{Email: "b@example.test"}}}, ModeReply, []Person{me}).To[0].Email != "b@example.test" {
		t.Error("a reply to one's own message did not go to its recipients")
	}
}

func TestBuildKeepsHeaderValuesOnOneLine(t *testing.T) {
	d := draft()
	d.Attachments[0].Name = "a\nb.txt"
	raw, err := Build(d, when, nil)
	if err != nil {
		t.Fatal(err)
	}
	o, err := Parse(raw, "Drafts", 1)
	if err != nil || len(o.Attachments) != 1 || o.Attachments[0].Name != "a b.txt" {
		t.Errorf("got %+v, %v", o, err)
	}
}

func TestHTMLToText(t *testing.T) {
	for in, want := range map[string]string{
		`<p>Hello <b>there</b></p><p></p><p>Second</p>`:                                                      "Hello there\n\nSecond",
		`<p>On Mon, Bob wrote:</p><blockquote>Shall we?<br>Yes<blockquote>earlier</blockquote></blockquote>`: "On Mon, Bob wrote:\n> Shall we?\n> Yes\n> > earlier",
		`<ul><li><p>one</p></li><li><p>two</p></li></ul><ol><li>a</li><li>b</li></ol>`:                       "- one\n- two\n1. a\n2. b",
		`<p>see <a href="https://x.org">site</a>, <a href="https://y.org">https://y.org</a></p>`:             "see site <https://x.org>, https://y.org",
	} {
		if got := HTMLToText(in); got != want {
			t.Errorf("HTMLToText(%s)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestRichDraft(t *testing.T) {
	d := draft()
	d.Text = "ignored"
	d.HTML = `<p>Hi <b>Bob</b></p><div data-signature=""><p>Alice</p></div><script>x()</script><img src="https://tracker.example/p.gif">`
	raw, err := Build(d, when, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromDraft(raw, "Drafts", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.HTML != `<p>Hi <b>Bob</b></p><div data-signature=""><p>Alice</p></div><img src="">` {
		t.Errorf("html %q", got.HTML)
	}
	if strings.TrimSpace(got.Text) != "Hi Bob\nAlice" {
		t.Errorf("text %q", got.Text)
	}
}

func TestRespondFromTheIdentityAddressed(t *testing.T) {
	ids := []Person{{Email: "alice@example.test"}, {Name: "Sales", Email: "sales@example.test"}}
	o := &Original{
		From: []Person{{Email: "bob@example.test"}}, To: []Person{{Email: "Sales@example.test"}},
		Cc: []Person{{Email: "alice@example.test"}, {Email: "carol@example.test"}}, Subject: "Order", Text: "Line one\nLine two",
	}
	r := Respond(o, ModeReplyAll, ids)
	if r.From.Email != "sales@example.test" || len(r.To) != 1 || len(r.Cc) != 1 || r.Cc[0].Email != "carol@example.test" {
		t.Errorf("reply all %+v", r)
	}
	if !strings.Contains(r.HTML, "<blockquote>Line one<br>Line two<br></blockquote>") {
		t.Errorf("quote html %q", r.HTML)
	}
	if Respond(&Original{From: []Person{{Email: "bob@example.test"}}}, ModeReply, ids).From.Email != "alice@example.test" {
		t.Error("a reply to mail sent to no identity is not from the login address")
	}
}

func TestInlineImageIsAnInlinePart(t *testing.T) {
	d := draft()
	d.HTML = `<p>Look</p><img src="cid:dot@postbox">`
	d.Attachments = append(d.Attachments, Attachment{Name: "dot.png", Type: "image/png", Data: []byte("png"), Inline: true, ContentID: "dot@postbox"})
	raw, err := Build(d, when, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("Content-Id: <dot@postbox>")) {
		t.Errorf("no Content-ID:\n%s", raw)
	}
	o, err := Parse(raw, "Drafts", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Attachments) != 2 || !o.Attachments[1].Inline || o.Attachments[1].ContentID != "dot@postbox" || o.Attachments[1].Name != "dot.png" || o.Attachments[1].Part != "3" {
		t.Errorf("attachments %+v", o.Attachments)
	}
	if bytes.Contains(raw, []byte("attachment; filename=dot.png")) {
		t.Error("the image is an attachment")
	}
}
