package mail

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func part(subtype, encoding, charset string) *imap.BodyStructureSinglePart {
	return &imap.BodyStructureSinglePart{Type: "text", Subtype: subtype, Encoding: encoding, Params: map[string]string{"charset": charset}}
}

func TestSnippet(t *testing.T) {
	long := strings.Repeat("word ", 100)
	b64 := base64.StdEncoding.EncodeToString([]byte("Hello from base64, café included."))
	tests := []struct {
		name string
		raw  string
		part *imap.BodyStructureSinglePart
		want string
	}{
		{"plain", "Hi Alice,\r\n\r\nThe fence  is   fixed.\r\n", part("plain", "7bit", "utf-8"), "Hi Alice, The fence is fixed."},
		{"quoted lines dropped", "Sounds good.\r\n> earlier text\r\n> more\r\n", part("plain", "7bit", "utf-8"), "Sounds good."},
		{"quoted-printable cut mid escape", "Caf=C3=A9 opens soon=\r\n and=C", part("plain", "quoted-printable", "utf-8"), "Café opens soon and"},
		{"base64 cut mid quantum", b64[:len(b64)-3], part("plain", "base64", "utf-8"), "Hello from base64, café included"},
		{"latin-1", "Caf\xe9", part("plain", "8bit", "iso-8859-1"), "Café"},
		{"html", "<html><head><style>p{}</style></head><body><p>Hello&nbsp;<b>there</b></p><script>x()</script></body></html>", part("html", "7bit", "utf-8"), "Hello there"},
		{"long text is cut", long, part("plain", "7bit", "utf-8"), strings.TrimSpace(long[:snippetRunes]) + "…"},
		{"invalid UTF-8 removed", "ok \xff\xfe there", part("plain", "8bit", "utf-8"), "ok there"},
	}
	for _, tt := range tests {
		if got := snippet([]byte(tt.raw), tt.part); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestTextPart(t *testing.T) {
	plain, html := part("plain", "7bit", "utf-8"), part("html", "7bit", "utf-8")
	attached := part("plain", "base64", "utf-8")
	attached.Extended = &imap.BodyStructureSinglePartExt{Disposition: &imap.BodyStructureDisposition{Value: "attachment"}}

	if path, p := textPart(plain); p != plain || path != nil {
		t.Errorf("single part: %v %v", path, p)
	}
	alt := &imap.BodyStructureMultiPart{Subtype: "alternative", Children: []imap.BodyStructure{html, plain}}
	if path, p := textPart(alt); p != plain || len(path) != 1 || path[0] != 2 {
		t.Errorf("alternative: %v %v", path, p)
	}
	onlyHTML := &imap.BodyStructureMultiPart{Subtype: "mixed", Children: []imap.BodyStructure{attached, html}}
	if path, p := textPart(onlyHTML); p != html || len(path) != 1 || path[0] != 2 {
		t.Errorf("html with a text attachment: %v %v", path, p)
	}
}
