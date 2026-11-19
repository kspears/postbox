package outbox

import (
	"strings"
	"testing"
)

func TestEnvelopeDropsBcc(t *testing.T) {
	raw := "From: Alice <alice@example.test>\r\nTo: bob@example.test\r\nBcc: dave@example.test,\r\n eve@example.test\r\nCc: carol@example.test\r\nSubject: hi\r\n\r\nBcc: this line is body text\r\n"
	env, out, err := envelope([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if env.From != "alice@example.test" || strings.Join(env.To, " ") != "bob@example.test carol@example.test dave@example.test eve@example.test" {
		t.Errorf("envelope %+v", env)
	}
	want := "From: Alice <alice@example.test>\r\nTo: bob@example.test\r\nCc: carol@example.test\r\nSubject: hi\r\n\r\nBcc: this line is body text\r\n"
	if string(out) != want {
		t.Errorf("got  %q\nwant %q", out, want)
	}
}

func TestEnvelopeNeedsRecipients(t *testing.T) {
	if _, _, err := envelope([]byte("From: a@example.test\r\nSubject: x\r\n\r\nbody")); err == nil {
		t.Error("a message without recipients was accepted")
	}
}
