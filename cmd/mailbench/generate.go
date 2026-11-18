package main

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"time"
)

type genMessage struct {
	when   time.Time
	id     string
	refs   []string
	thread int
	sent   bool // written by the account's owner, so it lives in Sent
}

// generate appends n messages to folder, grouped into conversations the way real mail is: most
// alone, some in long exchanges. About one reply in six is the owner's own and goes to Sent
// instead, which is what Postbox has to join back in. The same n always gives the same mailbox.
func generate(t target, folder string, n int) error {
	c, err := t.login(nil)
	if err != nil {
		return err
	}
	defer c.Close()
	// "Already exists" is fine: generating twice grows the folder.
	c.Create(folder, nil).Wait()

	rng := rand.New(rand.NewPCG(1, uint64(n)))
	words := vocabulary(rng, 4000)
	end := time.Now().Truncate(time.Hour)
	span := 5 * 365 * 24 * time.Hour

	var msgs []genMessage
	for thread := 0; len(msgs) < n; thread++ {
		size := 1
		if rng.IntN(100) >= 60 {
			for size = 2; size < 40 && rng.IntN(100) < 70; size++ {
			}
		}
		when := end.Add(-time.Duration(rng.Int64N(int64(span))))
		var refs []string
		for i := 0; i < size && len(msgs) < n; i++ {
			id := fmt.Sprintf("<t%d.m%d@mailbench.test>", thread, i)
			msgs = append(msgs, genMessage{
				when: when, id: id, refs: slices.Clone(refs), thread: thread,
				sent: i > 0 && rng.IntN(6) == 0,
			})
			refs = append(refs, id)
			// Replies follow within minutes to a few days, never into the future.
			when = when.Add(time.Duration(rng.Int64N(int64(72 * time.Hour))))
			if when.After(end) {
				when = end
			}
		}
	}
	// Arrival order, so UIDs rise with time as they do in a real mailbox.
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].when.Before(msgs[j].when) })

	start := time.Now()
	sent := 0
	for i, m := range msgs {
		dest := folder
		from, to := fmt.Sprintf("Sender %d <sender%d@example.org>", m.thread%500, m.thread%500), t.user
		if m.sent {
			dest, from, to = "Sent", t.user, from
			sent++
		}
		if _, err := appendMessage(c, dest, render(rng, words, m, from, to), m.when); err != nil {
			return fmt.Errorf("append message %d: %w", i, err)
		}
		if (i+1)%5000 == 0 {
			fmt.Printf("  %d of %d (%.0f/s)\n", i+1, n, float64(i+1)/time.Since(start).Seconds())
		}
	}
	fmt.Printf("generated %d messages in %s: %d in %s, %d in Sent\n", n, time.Since(start).Round(time.Second), n-sent, folder, sent)
	return nil
}

func vocabulary(rng *rand.Rand, n int) []string {
	syllables := strings.Fields("ba be bi bo bu da de di do du fa fe fi fo ga ge gi go ha he hi ho ka ke ki ko la le li lo lu ma me mi mo mu na ne ni no nu pa pe pi po ra re ri ro ru sa se si so su ta te ti to tu va ve vi vo za ze zi zo")
	words := make([]string, n)
	for i := range words {
		var b strings.Builder
		for range 2 + rng.IntN(3) {
			b.WriteString(syllables[rng.IntN(len(syllables))])
		}
		words[i] = b.String()
	}
	return words
}

func render(rng *rand.Rand, words []string, m genMessage, from, to string) []byte {
	// Low word numbers are common and high ones rare, roughly as in real text.
	word := func() string { return words[int(float64(len(words))*rng.Float64()*rng.Float64())] }
	text := func(count int) string {
		var b strings.Builder
		for i := range count {
			b.WriteString(word())
			if i%14 == 13 {
				b.WriteString(".\r\n")
			} else {
				b.WriteByte(' ')
			}
		}
		return b.String()
	}

	subject := fmt.Sprintf("Conversation %d about %s", m.thread, words[m.thread%len(words)])
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nDate: %s\r\nMessage-ID: %s\r\n", from, to, m.when.Format(time.RFC1123Z), m.id)
	if len(m.refs) > 0 {
		subject = "Re: " + subject
		fmt.Fprintf(&b, "In-Reply-To: %s\r\nReferences: %s\r\n", m.refs[len(m.refs)-1], strings.Join(m.refs, "\r\n "))
	}
	fmt.Fprintf(&b, "Subject: %s\r\nMIME-Version: 1.0\r\n", subject)

	body := text(120 + rng.IntN(600))
	if rng.IntN(100) < 30 {
		b.WriteString("Content-Type: multipart/alternative; boundary=\"mailbench\"\r\n\r\n")
		fmt.Fprintf(&b, "--mailbench\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", body)
		fmt.Fprintf(&b, "--mailbench\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<html><body><p>%s</p></body></html>\r\n", strings.ReplaceAll(body, ".\r\n", ".</p>\r\n<p>"))
		b.WriteString("--mailbench--\r\n")
	} else {
		fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", body)
	}
	return []byte(b.String())
}
