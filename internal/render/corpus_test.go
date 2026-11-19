package render

import (
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
)

// Every HTML part of the hostile corpus comes out without anything active or remote. The browser
// test (criterion 13) checks the same end to end; this one names the file and the leak.
func TestHostileCorpus(t *testing.T) {
	files, err := filepath.Glob("../../testdata/hostile/*.eml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus: %v", err)
	}
	// A link to the sink is fine: following one takes a click. Anything that would load from it
	// on its own is not.
	active := regexp.MustCompile(`(?i)<\s*(script|iframe|frame|object|embed|applet|form|input|button|meta|base|link|svg|math|video|audio|source|track|portal|isindex|area|map)\b|\son[a-z]+\s*=|javascript:|vbscript:|data:text|srcset|ping=|background=|poster=|(src|style)="[^"]*(sink|rl\()`)
	for _, f := range files {
		raw, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		entity, err := message.Read(raw)
		raw.Close()
		if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		parts := 0
		entity.Walk(func(_ []int, e *message.Entity, err error) error {
			mediaType, _, _ := mime.ParseMediaType(e.Header.Get("Content-Type"))
			if mediaType != "text/html" {
				return nil
			}
			body, _ := io.ReadAll(e.Body)
			parts++
			res := HTML(string(body), Options{})
			if strings.Contains(res.CSS, "sink") || strings.Contains(res.CSS, "rl(") {
				t.Errorf("%s: kept a remote reference in the style sheet: %s", filepath.Base(f), res.CSS)
			}
			for _, out := range []string{res.HTML} {
				if m := active.FindString(out); m != "" {
					i := strings.Index(out, m)
					t.Errorf("%s: kept %q in …%s…", filepath.Base(f), m, out[max(0, i-60):min(len(out), i+60)])
				}
			}
			return nil
		})
		if parts == 0 && !strings.Contains(f, "utf7") {
			t.Logf("%s: no HTML part read", filepath.Base(f))
		}
	}
}
