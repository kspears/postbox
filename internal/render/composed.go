package render

import (
	"net/url"

	"github.com/microcosm-cc/bluemonday"
)

var composedPolicy = newComposedPolicy()

// The markup the composer makes: text formatting, lists, links, quotes and images that are
// parts of the message itself (cid:). Signatures are kept in a div marked data-signature.
func newComposedPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements(
		"p", "br", "strong", "b", "em", "i", "u", "s", "a", "ul", "ol", "li", "blockquote",
		"h1", "h2", "h3", "pre", "code", "hr", "img", "div",
	)
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("src", "alt").OnElements("img")
	p.AllowAttrs("start").OnElements("ol")
	p.AllowAttrs("data-signature").OnElements("div")
	p.AllowURLSchemes("http", "https", "mailto", "cid")
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(false)
	// Only images in the message itself: a remote one would make the reader fetch something
	// the sender chose.
	p.RewriteSrc(func(u *url.URL) {
		if u.Scheme != "cid" {
			*u = url.URL{}
		}
	})
	p.SkipElementsContent("script", "style", "title", "head", "noscript", "template", "textarea", "xmp", "iframe", "object", "svg", "math")
	return p
}

// Composed sanitises HTML written in Postbox's composer, such as a signature or a message body,
// to the markup the composer itself makes.
func Composed(doc string) string {
	return composedPolicy.Sanitize(doc)
}
