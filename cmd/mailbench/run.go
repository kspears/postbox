package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const (
	reps     = 5
	pageSize = 50
)

// run times the server-side work behind the spec's speed targets, on connections that are
// already open and logged in (as Postbox keeps them) unless a line says otherwise. Each
// operation runs five times; the first is shown apart because it often pays for a cold cache.
func run(t target, folder string) error {
	c, err := t.login(nil)
	if err != nil {
		return err
	}
	defer c.Close()
	selected, err := c.Select(folder, nil).Wait()
	if err != nil {
		return fmt.Errorf("select %s (run `just bench generate` first): %w", folder, err)
	}
	fmt.Printf("%s: %d messages\n\n%-46s %9s %9s %9s\n", folder, selected.NumMessages, "", "first", "median", "slowest")

	all := &imap.SearchCriteria{}
	var failure error
	measure := func(name string, fn func() error) {
		if failure != nil {
			return
		}
		var took []time.Duration
		for range reps {
			start := time.Now()
			if err := fn(); err != nil {
				failure = fmt.Errorf("%s: %w", name, err)
				return
			}
			took = append(took, time.Since(start))
		}
		first := took[0]
		slices.Sort(took)
		fmt.Printf("%-46s %9s %9s %9s\n", name, ms(first), ms(took[reps/2]), ms(took[reps-1]))
	}

	measure("connect, log in, select (new connection)", func() error {
		fresh, err := t.login(nil)
		if err != nil {
			return err
		}
		defer fresh.Close()
		_, err = fresh.Select(folder, nil).Wait()
		return err
	})

	measure("folder list with unread counts", func() error {
		_, err := c.List("", "*", &imap.ListOptions{ReturnSpecialUse: true, ReturnStatus: &imap.StatusOptions{NumMessages: true, NumUnseen: true}}).Collect()
		return err
	})

	var threads []imapclient.ThreadData
	measure("thread the whole folder (UID THREAD REFS)", func() error {
		threads, err = c.UIDThread(&imapclient.ThreadOptions{Algorithm: "REFS", SearchCriteria: all}).Wait()
		return err
	})
	if failure != nil {
		return failure
	}
	fmt.Printf("  -> %d conversations\n", len(threads))

	// REFS orders conversations by their latest message, oldest first: the first page is the tail.
	page := threads[max(0, len(threads)-pageSize):]
	var pageUIDs imap.UIDSet
	var biggest []imap.UID
	for _, thread := range page {
		uids := threadUIDs(thread)
		pageUIDs.AddNum(uids...)
		if len(uids) > len(biggest) {
			biggest = uids
		}
	}
	listFetch := &imap.FetchOptions{UID: true, Flags: true, Envelope: true, InternalDate: true}
	measure(fmt.Sprintf("fetch the first page (%d conversations)", len(page)), func() error {
		_, err := c.Fetch(pageUIDs, listFetch).Collect()
		return err
	})
	measure("open the folder: thread + first page", func() error {
		if _, err := c.UIDThread(&imapclient.ThreadOptions{Algorithm: "REFS", SearchCriteria: all}).Wait(); err != nil {
			return err
		}
		_, err := c.Fetch(pageUIDs, listFetch).Collect()
		return err
	})
	measure("fallback: UID THREAD REFERENCES", func() error {
		_, err := c.UIDThread(&imapclient.ThreadOptions{Algorithm: imap.ThreadReferences, SearchCriteria: all}).Wait()
		return err
	})
	measure("fallback: UID SORT (REVERSE ARRIVAL)", func() error {
		_, err := c.UIDSort(&imapclient.SortOptions{SearchCriteria: all, SortCriteria: []imapclient.SortCriterion{{Key: imapclient.SortKeyArrival, Reverse: true}}}).Wait()
		return err
	})

	whole := &imap.FetchItemBodySection{Peek: true}
	// Opening a conversation: its bodies from this folder, plus the owner's replies from Sent.
	var rootID, word string
	measure(fmt.Sprintf("open a conversation (%d messages, bodies)", len(biggest)), func() error {
		msgs, err := c.Fetch(imap.UIDSetNum(biggest...), &imap.FetchOptions{
			UID: true, Flags: true, Envelope: true,
			BodySection: []*imap.FetchItemBodySection{whole},
		}).Collect()
		if err != nil {
			return err
		}
		if len(msgs) == 0 || msgs[0].Envelope == nil {
			return errors.New("the conversation came back empty")
		}
		rootID = msgs[0].Envelope.MessageID
		// A word that is certainly in the folder, for the searches below.
		_, body, _ := strings.Cut(string(msgs[0].FindBodySection(whole)), "\r\n\r\n")
		for _, w := range strings.Fields(body) {
			if len(w) >= 8 && strings.Trim(w, "abcdefghijklmnopqrstuvwxyz") == "" {
				word = w
				break
			}
		}
		return nil
	})
	sent, err := t.login(nil)
	if err != nil {
		return err
	}
	defer sent.Close()
	sentBox, err := sent.Select("Sent", nil).Wait()
	if err != nil {
		return fmt.Errorf("select Sent: %w", err)
	}
	found := 0
	measure(fmt.Sprintf("find its replies in Sent (%d messages)", sentBox.NumMessages), func() error {
		data, err := sent.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "References", Value: "<" + rootID + ">"}}}, nil).Wait()
		if err == nil {
			found = len(data.AllUIDs())
		}
		return err
	})
	if failure == nil {
		fmt.Printf("  -> %d replies\n", found)
	}

	if failure == nil && word == "" {
		return errors.New("found no word to search for in the opened conversation")
	}
	for _, term := range []struct{ label, text string }{
		{"body search, whole word", word},
		{"body search, middle of a word", word[min(1, len(word)):max(1, len(word)-1)]},
	} {
		hits := 0
		measure(term.label, func() error {
			data, err := c.UIDSearch(&imap.SearchCriteria{Text: []string{term.text}}, nil).Wait()
			if err == nil {
				hits = len(data.AllUIDs())
			}
			return err
		})
		if failure == nil {
			fmt.Printf("  -> %d messages match %q\n", hits, term.text)
		}
	}

	newest := imap.UIDSetNum(biggest[len(biggest)-1])
	seen := false
	measure("mark one message read or unread", func() error {
		op := imap.StoreFlagsAdd
		if seen {
			op = imap.StoreFlagsDel
		}
		seen = !seen
		_, err := c.Store(newest, &imap.StoreFlags{Op: op, Flags: []imap.Flag{imap.FlagSeen}}, nil).Collect()
		return err
	})
	return failure
}

func threadUIDs(t imapclient.ThreadData) []imap.UID {
	var uids []imap.UID
	for _, n := range t.Chain {
		uids = append(uids, imap.UID(n))
	}
	for _, sub := range t.SubThreads {
		uids = append(uids, threadUIDs(sub)...)
	}
	return uids
}

func ms(d time.Duration) string {
	return fmt.Sprintf("%.0f ms", float64(d.Microseconds())/1000)
}
