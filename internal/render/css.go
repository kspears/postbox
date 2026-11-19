// Package render turns untrusted message bodies into HTML that is safe to show: no script, no
// form, and no request to anywhere unless the reader allowed remote images.
package render

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

// Properties a message may style itself with. Anything else is dropped, including everything
// that can load a resource (content, cursor, behavior, -moz-binding, mask, filter) and position,
// which would let a message draw over the reading pane's own controls.
var allowedProperties = map[string]bool{}

func init() {
	for _, p := range strings.Fields(`
		color background background-color opacity
		font font-family font-size font-style font-variant font-weight
		text-align text-decoration text-decoration-color text-decoration-line text-decoration-style
		text-indent text-transform text-overflow text-shadow line-height letter-spacing
		word-spacing white-space vertical-align direction unicode-bidi word-break word-wrap
		overflow-wrap hyphens
		margin margin-top margin-right margin-bottom margin-left
		padding padding-top padding-right padding-bottom padding-left
		border border-top border-right border-bottom border-left border-color border-style
		border-width border-radius border-top-color border-right-color border-bottom-color
		border-left-color border-top-style border-right-style border-bottom-style
		border-left-style border-top-width border-right-width border-bottom-width
		border-left-width border-top-left-radius border-top-right-radius
		border-bottom-left-radius border-bottom-right-radius border-collapse border-spacing
		width height min-width min-height max-width max-height box-sizing
		display float clear overflow overflow-x overflow-y visibility
		table-layout caption-side empty-cells
		list-style list-style-type list-style-position
		gap row-gap column-gap flex flex-direction flex-wrap flex-basis flex-grow flex-shrink
		align-items align-self justify-content order`) {
		allowedProperties[p] = true
	}
}

var viewportUnit = regexp.MustCompile(`(?i)[0-9.](s|l|d)?v(h|w|i|b|min|max)$`)

// Functions a value may use. None of them can fetch anything; every other function (url,
// image-set, expression, element, paint and whatever comes next) drops the declaration.
var allowedFunctions = map[string]bool{}

func init() {
	for _, f := range strings.Fields(`rgb( rgba( hsl( hsla( hwb( lab( lch( oklab( oklch( color(
		calc( min( max( clamp( linear-gradient( radial-gradient( repeating-linear-gradient(
		repeating-radial-gradient( conic-gradient(`) {
		allowedFunctions[f] = true
	}
}

// safeValue reports whether a declaration's value tokens can be kept.
func safeValue(values []css.Token) bool {
	for _, v := range values {
		switch v.TokenType {
		case css.URLToken, css.BadURLToken, css.BadStringToken:
			return false
		case css.DimensionToken:
			// Lengths relative to the viewport would track the frame, which is sized to fit the
			// message: each would grow the other without end.
			if viewportUnit.Match(v.Data) {
				return false
			}
		case css.LeftParenthesisToken:
			// A bracket not opened by an allowed function: "url/**/(…)" and its relatives.
			return false
		case css.FunctionToken:
			if !allowedFunctions[strings.ToLower(string(v.Data))] {
				return false
			}
		}
		// A backslash escape can spell a blocked function in a way the lexer does not undo.
		if bytes.ContainsRune(v.Data, '\\') {
			return false
		}
	}
	return true
}

func writeTokens(b *strings.Builder, tokens []css.Token) {
	for _, t := range tokens {
		b.Write(t.Data)
	}
}

// FilterStyleSheet keeps the rules and declarations of a <style> block that cannot load or run
// anything: @import, @font-face and every at-rule but @media go, and so does any declaration
// outside the allow list or with a URL in it.
func FilterStyleSheet(sheet string) string {
	p := css.NewParser(parse.NewInputString(sheet), false)
	var b strings.Builder
	var selector strings.Builder
	skip := 0 // depth inside an at-rule or rule that is being dropped
	open := 0 // blocks whose opening brace was written
	for {
		gt, _, data := p.Next()
		if gt == css.ErrorGrammar {
			if p.Err() == io.EOF {
				break
			}
			continue
		}
		switch gt {
		case css.BeginAtRuleGrammar:
			if skip > 0 || !strings.EqualFold(string(data), "@media") {
				skip++
				continue
			}
			b.WriteString("@media")
			writeTokens(&b, p.Values())
			b.WriteString("{")
			open++
		case css.EndAtRuleGrammar, css.EndRulesetGrammar:
			if skip > 0 {
				skip--
				continue
			}
			if open > 0 {
				open--
				b.WriteString("}")
			}
		case css.QualifiedRuleGrammar:
			if skip == 0 {
				selector.Write(data)
				writeTokens(&selector, p.Values())
				selector.WriteString(",")
			}
		case css.BeginRulesetGrammar:
			if skip > 0 {
				skip++
				continue
			}
			selector.Write(data)
			writeTokens(&selector, p.Values())
			b.WriteString(selector.String())
			selector.Reset()
			b.WriteString("{")
			open++
		case css.DeclarationGrammar:
			if skip == 0 {
				writeDeclaration(&b, data, p.Values())
			}
		}
		// AtRuleGrammar (@import, @charset, @namespace), comments, custom properties and stray
		// tokens are dropped.
	}
	return escapeForStyleElement(b.String())
}

// FilterInlineStyle filters a style attribute's declarations the same way.
func FilterInlineStyle(style string) string {
	p := css.NewParser(parse.NewInputString(style), true)
	var b strings.Builder
	for {
		gt, _, data := p.Next()
		if gt == css.ErrorGrammar {
			if p.Err() == io.EOF {
				break
			}
			continue
		}
		if gt == css.DeclarationGrammar {
			writeDeclaration(&b, data, p.Values())
		}
	}
	return b.String()
}

func writeDeclaration(b *strings.Builder, property []byte, values []css.Token) {
	name := strings.ToLower(string(property))
	if !allowedProperties[name] || !safeValue(values) {
		return
	}
	b.WriteString(name)
	b.WriteString(":")
	writeTokens(b, values)
	b.WriteString(";")
}

// escapeForStyleElement makes sure the filtered sheet cannot close the <style> element it is
// written into. "<" only appears in CSS inside strings, where its escape means the same.
func escapeForStyleElement(s string) string {
	return strings.ReplaceAll(s, "<", `\3c `)
}
