package render

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Options say what a rendered body may show.
type Options struct {
	// RemoteImages lets <img> keep http and https sources. Nothing else remote is ever kept.
	RemoteImages bool
	// Inline maps a Content-ID (without angle brackets) to a data: URL for an image that came in
	// the message itself.
	Inline map[string]string
}

// The most embedded-image data one body may carry once rendered. Every reference to an image
// repeats it, so without a limit a small message referring to one image many times would need
// gigabytes.
const maxInlineOutput = 24 << 20

// Result is a rendered body.
type Result struct {
	HTML string // a fragment for <body>
	CSS  string // filtered <style> blocks, for <head>
	// RemoteImages counts remote images left out because Options.RemoteImages was false.
	RemoteImages int
}

var policy = newPolicy()

func newPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowStandardAttributes()
	p.AllowElements(
		"a", "abbr", "address", "article", "aside", "b", "bdi", "bdo", "big", "blockquote", "br",
		"caption", "center", "cite", "code", "col", "colgroup", "dd", "del", "details", "dfn",
		"div", "dl", "dt", "em", "figcaption", "figure", "font", "footer", "h1", "h2", "h3", "h4",
		"h5", "h6", "header", "hr", "i", "img", "ins", "kbd", "li", "main", "mark", "nav", "ol",
		"p", "pre", "q", "rp", "rt", "ruby", "s", "samp", "section", "small", "span", "strike",
		"strong", "sub", "summary", "sup", "table", "tbody", "td", "tfoot", "th", "thead", "time",
		"tr", "tt", "u", "ul", "var", "wbr",
	)
	p.AllowAttrs("class", "style").Globally()
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("src", "alt", "width", "height", "align", "border", "hspace", "vspace").OnElements("img")
	p.AllowAttrs("align", "valign", "width", "height", "bgcolor", "border", "cellpadding",
		"cellspacing", "colspan", "rowspan", "nowrap").
		OnElements("table", "thead", "tbody", "tfoot", "tr", "td", "th", "col", "colgroup", "caption")
	p.AllowAttrs("align").OnElements("p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "center", "hr")
	p.AllowAttrs("color", "face", "size").OnElements("font")
	p.AllowAttrs("start", "type").OnElements("ol")
	p.AllowAttrs("type").OnElements("ul", "li")
	p.AllowAttrs("open").OnElements("details")
	p.AllowAttrs("datetime").OnElements("time", "del", "ins")
	p.AllowURLSchemes("http", "https", "mailto", "cid")
	p.AllowURLSchemeWithCustomPolicy("data", func(u *url.URL) bool {
		return dataImage.MatchString(u.Opaque)
	})
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(false)
	// The text of these is never message content, so it is dropped with the element.
	p.SkipElementsContent("script", "style", "title", "head", "noscript", "template", "textarea", "xmp", "iframe", "object", "svg", "math")
	return p
}

// A data: URL that holds a bitmap image. SVG is left out: it can carry script and references.
var dataImage = regexp.MustCompile(`^image/(png|gif|jpeg|jpg|webp|bmp);base64,[A-Za-z0-9+/=\s]*$`)

// HTML sanitises an HTML body. Elements and attributes go through an allow list (bluemonday),
// CSS through FilterStyleSheet and FilterInlineStyle, and images are then made local or blocked
// according to opts.
func HTML(doc string, opts Options) Result {
	var res Result
	res.CSS = FilterStyleSheet(styleBlocks(doc))
	clean := policy.Sanitize(doc)
	res.HTML = rewrite(clean, opts, &res.RemoteImages)
	return res
}

// styleBlocks returns the contents of every <style> element, as an HTML parser sees them.
func styleBlocks(doc string) string {
	var b strings.Builder
	z := nethtml.NewTokenizer(strings.NewReader(doc))
	inStyle := false
	for {
		switch z.Next() {
		case nethtml.ErrorToken:
			return b.String()
		case nethtml.StartTagToken:
			name, _ := z.TagName()
			inStyle = atom.Lookup(name) == atom.Style
		case nethtml.EndTagToken:
			inStyle = false
		case nethtml.TextToken:
			if inStyle {
				b.Write(z.Text())
				b.WriteString("\n")
			}
		}
	}
}

// rewrite walks the sanitised fragment once more: style attributes are filtered, links open in
// a new tab without a referrer or an opener, and image sources become inline data, stay remote
// (when allowed), or go.
func rewrite(fragment string, opts Options, blocked *int) string {
	var b strings.Builder
	budget := maxInlineOutput
	z := nethtml.NewTokenizer(strings.NewReader(fragment))
	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			return b.String()
		}
		t := z.Token()
		if tt == nethtml.StartTagToken || tt == nethtml.SelfClosingTagToken {
			t.Attr = rewriteAttrs(t, opts, blocked, &budget)
		}
		b.WriteString(t.String())
	}
}

func rewriteAttrs(t nethtml.Token, opts Options, blocked *int, budget *int) []nethtml.Attribute {
	out := t.Attr[:0]
	for _, a := range t.Attr {
		switch {
		case a.Key == "style":
			a.Val = FilterInlineStyle(a.Val)
			if a.Val == "" {
				continue
			}
		case t.DataAtom == atom.Img && a.Key == "src":
			src, ok := imageSource(a.Val, opts, budget)
			if !ok {
				if strings.HasPrefix(strings.ToLower(a.Val), "http") {
					*blocked++
				}
				continue
			}
			a.Val = src
		case t.DataAtom == atom.A && (a.Key == "target" || a.Key == "rel"):
			continue // replaced below
		}
		out = append(out, a)
	}
	if t.DataAtom == atom.A {
		out = append(out,
			nethtml.Attribute{Key: "target", Val: "_blank"},
			nethtml.Attribute{Key: "rel", Val: "noopener noreferrer"})
	}
	return out
}

func imageSource(src string, opts Options, budget *int) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "cid":
		id := strings.Trim(u.Opaque, "<>")
		if decoded, err := url.PathUnescape(id); err == nil {
			id = decoded
		}
		data, ok := opts.Inline[id]
		if !ok || len(data) > *budget {
			return "", false
		}
		*budget -= len(data)
		return data, true
	case "data":
		return src, dataImage.MatchString(u.Opaque)
	case "http", "https":
		return u.String(), opts.RemoteImages
	}
	return "", false
}

var link = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// Text renders a plain-text body as HTML: escaped, line breaks kept, web addresses linked.
func Text(body string) Result {
	var b strings.Builder
	b.WriteString(`<div class="postbox-plain">`)
	last := 0
	for _, m := range link.FindAllStringIndex(body, -1) {
		b.WriteString(html.EscapeString(body[last:m[0]]))
		u := body[m[0]:m[1]]
		b.WriteString(`<a href="` + html.EscapeString(u) + `" target="_blank" rel="noopener noreferrer">` + html.EscapeString(u) + `</a>`)
		last = m[1]
	}
	b.WriteString(html.EscapeString(body[last:]))
	b.WriteString(`</div>`)
	return Result{HTML: b.String()}
}
