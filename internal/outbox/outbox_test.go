package outbox

import (
	"slices"
	"strings"
	"testing"

	"github.com/kspears/postbox/internal/store"
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

func TestRecipientsKeepNames(t *testing.T) {
	raw := "From: a@example.test\r\nTo: Bob Brown <bob@example.test>, plain@example.test\r\nCc: \"Carol, Jr\" <carol@example.test>\r\nBcc: Dave <dave@example.test>\r\n\r\nbody"
	got := recipients([]byte(raw))
	want := []store.Address{{Name: "Bob Brown", Email: "bob@example.test"}, {Email: "plain@example.test"}, {Name: "Carol, Jr", Email: "carol@example.test"}, {Name: "Dave", Email: "dave@example.test"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
