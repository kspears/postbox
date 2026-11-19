package compose

import (
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
// forward. me is the account's own address, left out of a reply to everyone.
func Respond(o *Original, mode string, me Person) *Draft {
	d := NewDraft(me)
	if mode == ModeForward {
		d.Subject = prefixed("Fwd:", o.Subject)
		d.Text = "\n\n---------- Forwarded message ----------\n" +
			"From: " + list(o.From) + "\n" +
			"Date: " + o.Date + "\n" +
			"Subject: " + o.Subject + "\n" +
			"To: " + list(o.To) + "\n" +
			ccLine(o.Cc) + "\n" + o.Text
		d.Attachments = append(d.Attachments, o.Attachments...)
		return d
	}

	d.Subject = prefixed("Re:", o.Subject)
	d.To = slices.Clone(o.ReplyTo)
	if len(d.To) == 0 {
		d.To = slices.Clone(o.From)
	}
	if mode == ModeReplyAll {
		d.To = unique(append(d.To, o.To...), me, nil)
		d.Cc = unique(slices.Clone(o.Cc), me, d.To)
	} else {
		d.To = unique(d.To, me, nil)
	}
	// Replying to your own message goes to whoever it was sent to, or, when that was only you,
	// back to you.
	if len(d.To) == 0 {
		d.To = unique(slices.Clone(o.To), me, nil)
	}
	if len(d.To) == 0 {
		d.To = slices.Clone(o.From)
	}
	if o.MessageID != "" {
		d.InReplyTo = o.MessageID
		d.References = append(slices.Clone(o.References), o.MessageID)
	}
	var b strings.Builder
	b.WriteString("\n\nOn " + o.Date + ", " + list(o.From) + " wrote:\n")
	for _, line := range strings.Split(strings.TrimRight(o.Text, "\n"), "\n") {
		if strings.HasPrefix(line, ">") {
			b.WriteString(">" + line + "\n")
		} else {
			b.WriteString("> " + line + "\n")
		}
	}
	d.Text = b.String()
	return d
}

func ccLine(cc []Person) string {
	if len(cc) == 0 {
		return ""
	}
	return "Cc: " + list(cc) + "\n"
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

// unique drops me, anyone in already, and repeats, comparing addresses without regard to case.
func unique(people []Person, me Person, already []Person) []Person {
	seen := map[string]bool{strings.ToLower(me.Email): true}
	for _, p := range already {
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
