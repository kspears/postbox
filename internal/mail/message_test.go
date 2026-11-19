package mail

import (
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func single(typ, subtype string, opts ...func(*imap.BodyStructureSinglePart)) *imap.BodyStructureSinglePart {
	p := &imap.BodyStructureSinglePart{Type: typ, Subtype: subtype, Params: map[string]string{}}
	for _, o := range opts {
		o(p)
	}
	return p
}

func withID(id string) func(*imap.BodyStructureSinglePart) {
	return func(p *imap.BodyStructureSinglePart) { p.ID = "<" + id + ">" }
}

func disposition(value, name string) func(*imap.BodyStructureSinglePart) {
	return func(p *imap.BodyStructureSinglePart) {
		p.Extended = &imap.BodyStructureSinglePartExt{Disposition: &imap.BodyStructureDisposition{Value: value, Params: map[string]string{"filename": name}}}
	}
}

func multi(subtype string, children ...imap.BodyStructure) *imap.BodyStructureMultiPart {
	return &imap.BodyStructureMultiPart{Subtype: subtype, Children: children}
}

func parts(l layout) []string {
	var out []string
	for _, a := range l.attachments {
		out = append(out, a.Part)
	}
	return out
}

func TestPlan(t *testing.T) {
	t.Run("single text part", func(t *testing.T) {
		l := plan(single("text", "plain"))
		if l.bodyPart == nil || l.body != nil || len(l.attachments) != 0 {
			t.Errorf("got %+v", l)
		}
	})
	t.Run("alternative prefers html and does not list the plain copy", func(t *testing.T) {
		l := plan(multi("alternative", single("text", "plain"), single("text", "html")))
		if !slices.Equal(l.body, []int{2}) || len(l.attachments) != 0 {
			t.Errorf("body %v, attachments %v", l.body, parts(l))
		}
	})
	t.Run("related html keeps its embedded image inline", func(t *testing.T) {
		l := plan(multi("related", single("text", "html"), single("image", "png", withID("logo"))))
		if _, ok := l.inline["logo"]; !ok || len(l.attachments) != 0 {
			t.Errorf("inline %v, attachments %v", l.inline, parts(l))
		}
	})
	t.Run("an image with a Content-ID beside a plain text body is offered", func(t *testing.T) {
		l := plan(multi("mixed", single("text", "plain"), single("image", "jpeg", withID("photo"), disposition("inline", "photo.jpg"))))
		if len(l.inline) != 0 || !slices.Equal(parts(l), []string{"2"}) || l.attachments[0].Name != "photo.jpg" {
			t.Errorf("inline %v, attachments %+v", l.inline, l.attachments)
		}
	})
	t.Run("text, image, text: the second text part is offered", func(t *testing.T) {
		l := plan(multi("mixed", single("text", "plain"), single("image", "png", disposition("inline", "a.png")), single("text", "plain")))
		if !slices.Equal(l.body, []int{1}) || !slices.Equal(parts(l), []string{"2", "3"}) {
			t.Errorf("body %v, attachments %v", l.body, parts(l))
		}
	})
	t.Run("attachments and forwarded messages", func(t *testing.T) {
		fwd := single("message", "rfc822")
		fwd.MessageRFC822 = &imap.BodyStructureMessageRFC822{BodyStructure: single("text", "html")}
		l := plan(multi("mixed", single("text", "plain"), single("application", "pdf", disposition("attachment", "a.pdf")), fwd))
		if !slices.Equal(parts(l), []string{"2", "3"}) || l.attachments[1].Name != "message.eml" {
			t.Errorf("attachments %+v", l.attachments)
		}
	})
	t.Run("no body at all", func(t *testing.T) {
		l := plan(multi("mixed", single("application", "zip", disposition("attachment", "a.zip"))))
		if l.bodyPart != nil || !slices.Equal(parts(l), []string{"1"}) {
			t.Errorf("got %+v", l)
		}
	})
}
