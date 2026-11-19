package render

import (
	"regexp"
	"strings"
	"testing"
)

func TestHTMLRemovesActiveContent(t *testing.T) {
	cases := map[string]string{
		"script":          `<p>hi</p><script>alert(1)</script>`,
		"event handler":   `<img src="data:image/png;base64,AAAA" onerror="alert(1)">`,
		"javascript link": `<a href="javascript:alert(1)">x</a>`,
		"entity scheme":   `<a href="&#106;avascript:alert(1)">x</a>`,
		"vbscript":        `<a href="vbscript:msgbox(1)">x</a>`,
		"data html link":  `<a href="data:text/html,<script>alert(1)</script>">x</a>`,
		"iframe":          `<iframe src="http://sink/x"></iframe>`,
		"form":            `<form action="http://sink/x"><input name=a><button>go</button></form>`,
		"meta refresh":    `<meta http-equiv="refresh" content="0;url=http://sink/x">`,
		"base":            `<base href="http://sink/"><img src="x.png">`,
		"link stylesheet": `<link rel="stylesheet" href="http://sink/x.css">`,
		"svg":             `<svg onload="alert(1)"><image href="http://sink/x"/></svg>`,
		"style breakout":  `<style>p{color:red}</style><img src=x onerror=alert(1)><style>`,
		"srcset":          `<img srcset="http://sink/x 1x">`,
		"background attr": `<table background="http://sink/x"><tr><td background="http://sink/y">a</td></tr></table>`,
		"video poster":    `<video poster="http://sink/x"></video>`,
		"ping":            `<a href="https://example.org" ping="http://sink/x">x</a>`,
		"comment hiding":  `<!--><img src=x onerror=alert(1)>-->`,
		"noscript mxss":   `<noscript><p title="</noscript><img src=x onerror=alert(1)>">`,
		"svg data image":  `<img src="data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=">`,
	}
	bad := regexp.MustCompile(`(?i)<script|<iframe|<form|<input|<meta|<base|<link|<svg|<video|\bon[a-z]+\s*=|javascript:|vbscript:|data:text|sink|srcset|background=|ping=|image/svg`)
	for name, in := range cases {
		got := HTML(in, Options{}).HTML
		if m := bad.FindString(got); m != "" {
			t.Errorf("%s: output still has %q: %s", name, m, got)
		}
	}
}

func TestStyleSheetFilter(t *testing.T) {
	in := `
		@import url(http://sink/a.css);
		@import "http://sink/b.css";
		@font-face { font-family: x; src: url(http://sink/f.woff) }
		@namespace svg url(http://www.w3.org/2000/svg);
		p, .lead { color: red; background: url(http://sink/bg.png); font-weight: bold }
		td { background-image: image-set("http://sink/i.png" 1x); padding: 4px }
		a { behavior: url(x.htc); -moz-binding: url(x.xml); cursor: url(http://sink/c), auto }
		div { width: expression(alert(1)); background: \75 rl(http://sink/e) }
		@media (max-width: 600px) { p { font-size: 14px; list-style-image: url(http://sink/l) } }
		@supports (display: grid) { p { background: url(http://sink/s) } }
		h1 { content: "</style><script>alert(1)</script>"; color: blue }`
	got := FilterStyleSheet(in)
	for _, banned := range []string{"sink", "url(", "image-set", "expression", "behavior", "binding", "@import", "@font-face", "@namespace", "@supports", "</style", "\\75"} {
		if strings.Contains(got, banned) {
			t.Errorf("kept %q in %q", banned, got)
		}
	}
	for _, kept := range []string{"p,.lead{color:red;font-weight:bold;}", "td{padding:4px;}", "@media", "font-size:14px", "h1{color:blue;}"} {
		if !strings.Contains(got, kept) {
			t.Errorf("lost %q from %q", kept, got)
		}
	}
}

func TestInlineStyleFilter(t *testing.T) {
	got := FilterInlineStyle(`color: red; background: url(http://sink/x); position: fixed; width: 10px`)
	if got != "color:red;width:10px;" {
		t.Errorf("got %q", got)
	}
}

func TestImages(t *testing.T) {
	doc := `<img src="cid:logo@example.org"><img src="http://sink/remote.png"><img src="cid:missing">`
	inline := map[string]string{"logo@example.org": "data:image/png;base64,AAAA"}

	blocked := HTML(doc, Options{Inline: inline})
	if blocked.RemoteImages != 1 || strings.Contains(blocked.HTML, "sink") {
		t.Errorf("remote image not blocked: %+v", blocked)
	}
	if !strings.Contains(blocked.HTML, `src="data:image/png;base64,AAAA"`) {
		t.Errorf("embedded image not inlined: %s", blocked.HTML)
	}
	if strings.Contains(blocked.HTML, "cid:") {
		t.Errorf("unresolved cid: kept: %s", blocked.HTML)
	}

	allowed := HTML(doc, Options{Inline: inline, RemoteImages: true})
	if allowed.RemoteImages != 0 || !strings.Contains(allowed.HTML, `src="http://sink/remote.png"`) {
		t.Errorf("allowed remote image missing: %+v", allowed)
	}
}

func TestLinksOpenInANewTabWithoutReferrer(t *testing.T) {
	got := HTML(`<a href="https://example.org/a" target="_self" rel="opener">a</a> <a href="mailto:x@example.org">m</a>`, Options{}).HTML
	if strings.Count(got, `target="_blank"`) != 2 || strings.Count(got, `rel="noopener noreferrer"`) != 2 || strings.Contains(got, "_self") || strings.Contains(got, `"opener"`) {
		t.Errorf("got %s", got)
	}
}

func TestKeepsOrdinaryFormatting(t *testing.T) {
	in := `<table width="600" cellpadding="4"><tr><td style="color:#333;padding:8px" align="center"><h1>Hello</h1><p class="lead">Text <b>bold</b> <a href="https://example.org">link</a></p></td></tr></table>`
	got := HTML(in, Options{}).HTML
	for _, want := range []string{`<table width="600" cellpadding="4">`, `style="color:#333;padding:8px;"`, `align="center"`, `<h1>Hello</h1>`, `class="lead"`, `<b>bold</b>`} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q: %s", want, got)
		}
	}
}

func TestRepeatedEmbeddedImagesStayWithinBudget(t *testing.T) {
	image := "data:image/png;base64," + strings.Repeat("A", 1<<20)
	doc := strings.Repeat(`<img src="cid:a">`, 100)
	got := HTML(doc, Options{Inline: map[string]string{"a": image}}).HTML
	if len(got) > maxInlineOutput+len(doc) {
		t.Errorf("rendered %d bytes for one 1 MiB image referred to 100 times", len(got))
	}
	if strings.Count(got, image) == 0 {
		t.Error("the image was not shown at all")
	}
}

func TestViewportUnitsAreDropped(t *testing.T) {
	got := FilterInlineStyle(`min-height: 100vh; height: 50dvh; width: 10vmax; margin: 4px; padding: 2em`)
	if got != "margin:4px;padding:2em;" {
		t.Errorf("got %q", got)
	}
}

func TestText(t *testing.T) {
	got := Text("Hi <you>,\nsee https://example.org/a?b=1&c=2.\n").HTML
	want := `<div class="postbox-plain">Hi &lt;you&gt;,` + "\n" + `see <a href="https://example.org/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer">https://example.org/a?b=1&amp;c=2</a>.` + "\n" + `</div>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
