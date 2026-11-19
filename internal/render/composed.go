package render

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

var (
	composedPolicy = newComposedPolicy()
	cidURL         = regexp.MustCompile(`^cid:[^\s"'<>]+$`)
	// An image tag in the sanitiser's output, whose attribute values have < and > escaped.
	imgTag = regexp.MustCompile(`<img\b[^>]*>`)
)

// The markup the composer makes: text formatting, lists, links, quotes and images that are
// parts of the message itself (cid:). Signatures are kept in a div marked data-signature.
func newComposedPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements(
		"p", "br", "strong", "b", "em", "i", "u", "s", "a", "ul", "ol", "li", "blockquote",
		"h1", "h2", "h3", "h4", "h5", "h6", "pre", "code", "hr", "img", "div",
	)
	p.AllowAttrs("href").OnElements("a")
	// Only images in the message itself: a remote one would make the reader fetch something
	// the sender chose. An image left without a source is dropped.
	p.AllowAttrs("src").Matching(cidURL).OnElements("img")
	p.AllowAttrs("alt").OnElements("img")
	p.AllowAttrs("start").OnElements("ol")
	p.AllowAttrs("data-signature").OnElements("div")
	p.AllowURLSchemes("http", "https", "mailto", "cid")
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(false)
	p.SkipElementsContent(notContent...)
	return p
}

// Composed sanitises HTML written in Postbox's composer, such as a signature or a message body,
// to the markup the composer itself makes.
func Composed(doc string) string {
	return imgTag.ReplaceAllStringFunc(composedPolicy.Sanitize(doc), func(tag string) string {
		if !strings.Contains(tag, ` src="`) {
			return ""
		}
		return tag
	})
}
