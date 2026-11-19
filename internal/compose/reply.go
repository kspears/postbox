package compose

import (
	"html"
	"slices"
	"strings"
)

// Reply modes.
const (
	ModeReply    = "reply"
	ModeReplyAll = "replyAll"
	ModeForward  = "forward"
)

// Respond starts a draft answering o: a reply to its sender, a reply to everyone, or a
// forward, with the quoted text both as plain text and as HTML for the rich-text editor.
// identities are the person's own addresses, the login address first. The draft is from the
// one o was sent to, or the login address; a reply to everyone leaves them all out.
func Respond(o *Original, mode string, identities []Person) *Draft {
	d := NewDraft(addressedTo(o, identities))
	if mode == ModeForward {
		d.Subject = prefixed("Fwd:", o.Subject)
		header := []string{
			"---------- Forwarded message ----------",
			"From: " + list(o.From),
			"Date: " + o.Date,
			"Subject: " + o.Subject,
			"To: " + list(o.To),
		}
		if len(o.Cc) > 0 {
			header = append(header, "Cc: "+list(o.Cc))
		}
		d.Text = "\n\n" + strings.Join(header, "\n") + "\n\n" + o.Text
		d.HTML = "<p></p><p>" + strings.Join(escapeAll(header), "<br>") + "</p>" + TextToHTML(o.Text)
		// The quote is the original's text, so images it showed in its body travel as files.
		for _, a := range o.Attachments {
			a.Inline, a.ContentID = false, ""
			d.Attachments = append(d.Attachments, a)
		}
		return d
	}

	d.Subject = prefixed("Re:", o.Subject)
	d.To = slices.Clone(o.ReplyTo)
	if len(d.To) == 0 {
		d.To = slices.Clone(o.From)
	}
	if mode == ModeReplyAll {
		d.To = unique(append(d.To, o.To...), identities, nil)
		d.Cc = unique(slices.Clone(o.Cc), identities, d.To)
	} else {
		d.To = unique(d.To, identities, nil)
	}
	// Replying to your own message goes to whoever it was sent to, or, when that was only you,
	// back to you.
	if len(d.To) == 0 {
		d.To = unique(slices.Clone(o.To), identities, nil)
	}
	if len(d.To) == 0 {
		d.To = slices.Clone(o.From)
	}
	if o.MessageID != "" {
		d.InReplyTo = o.MessageID
		d.References = append(slices.Clone(o.References), o.MessageID)
	}
	intro := "On " + o.Date + ", " + list(o.From) + " wrote:"
	var b strings.Builder
	b.WriteString("\n\n" + intro + "\n")
	for _, line := range strings.Split(strings.TrimRight(o.Text, "\n"), "\n") {
		if strings.HasPrefix(line, ">") {
			b.WriteString(">" + line + "\n")
		} else {
			b.WriteString("> " + line + "\n")
		}
	}
	d.Text = b.String()
	d.HTML = "<p></p><p>" + html.EscapeString(intro) + "</p><blockquote>" + TextToHTML(strings.TrimRight(o.Text, "\n")) + "</blockquote>"
	return d
}

// addressedTo is the identity o was sent to, one in To before one in Cc, or the login address.
func addressedTo(o *Original, identities []Person) Person {
	for _, list := range [][]Person{o.To, o.Cc} {
		for _, p := range list {
			for _, id := range identities {
				if strings.EqualFold(p.Email, id.Email) {
					return id
				}
			}
		}
	}
	return identities[0]
}

func escapeAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = html.EscapeString(l)
	}
	return out
}

// prefixed puts prefix ("Re:", "Fwd:") before subject unless it is there already.
func prefixed(prefix, subject string) string {
	if strings.HasPrefix(strings.ToLower(subject), strings.ToLower(prefix)) {
		return subject
	}
	return strings.TrimSpace(prefix + " " + subject)
}

func list(people []Person) string {
	parts := make([]string, len(people))
	for i, p := range people {
		parts[i] = Address(p)
	}
	return strings.Join(parts, ", ")
}

// unique drops the person's own addresses, anyone in already, and repeats, comparing addresses
// without regard to case.
func unique(people []Person, own []Person, already []Person) []Person {
	seen := map[string]bool{}
	for _, p := range slices.Concat(own, already) {
		seen[strings.ToLower(p.Email)] = true
	}
	out := []Person{}
	for _, p := range people {
		key := strings.ToLower(p.Email)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}
