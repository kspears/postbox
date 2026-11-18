package mail

import (
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// PageSize is how many conversations one page of the list holds.
const PageSize = 50

// Person is a name and address from a message header.
type Person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Conversation is one row of the message list: a thread's messages in the open folder.
type Conversation struct {
	ID           imap.UID   `json:"id"` // the oldest message's UID; stable as the thread grows
	UIDs         []imap.UID `json:"uids"`
	Subject      string     `json:"subject"`
	Participants []Person   `json:"participants"` // senders, in the order they first wrote
	Snippet      string     `json:"snippet"`      // the start of the latest message
	Date         time.Time  `json:"date"`         // when the latest message arrived
	Count        int        `json:"count"`
	Unread       bool       `json:"unread"`
	Flagged      bool       `json:"flagged"`
	Attachments  bool       `json:"attachments"`
}

// Page is one page of conversations. Next is the cursor for the page after it, or 0 at the end.
type Page struct {
	Conversations []Conversation `json:"conversations"`
	Next          imap.UID       `json:"next,omitempty"`
}

// Select opens folder on the connection unless it is open already.
func (c *Conn) Select(folder string) (*imapclient.SelectedMailbox, error) {
	if m := c.Mailbox(); m != nil && m.Name == folder {
		return m, nil
	}
	if _, err := c.Client.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}
	return c.Mailbox(), nil
}

// Conversations returns the page of folder's conversations that follows the one whose latest
// message is cursor, newest first; cursor 0 means the first page.
//
// The cursor is a message rather than a position because new mail moves conversations up
// while someone scrolls. If the cursor's conversation has moved or gone, the page continues
// with the first conversation whose latest message is older than the cursor.
func (c *Conn) Conversations(folder string, cursor imap.UID) (*Page, error) {
	box, err := c.Select(folder)
	if err != nil {
		return nil, err
	}
	page := &Page{Conversations: []Conversation{}}
	if box.NumMessages == 0 {
		return page, nil
	}
	threads, err := c.threads()
	if err != nil {
		return nil, err
	}

	shown, next := pageOf(threads, cursor, PageSize)
	page.Next = next
	if len(shown) == 0 {
		return page, nil
	}

	page.Conversations, err = c.summarise(shown)
	return page, err
}

// pageOf picks up to size threads after the one whose latest message is cursor, and returns
// the cursor for the page after them (0 when none follows).
func pageOf(threads [][]imap.UID, cursor imap.UID, size int) (shown [][]imap.UID, next imap.UID) {
	start := 0
	if cursor != 0 {
		start = slices.IndexFunc(threads, func(t []imap.UID) bool { return slices.Max(t) == cursor }) + 1
		if start == 0 {
			// The cursor's conversation moved or went: continue below it.
			start = slices.IndexFunc(threads, func(t []imap.UID) bool { return slices.Max(t) < cursor })
			if start < 0 {
				return nil, 0
			}
		}
	}
	end := min(start+size, len(threads))
	shown = threads[start:end]
	if end < len(threads) && len(shown) > 0 {
		next = slices.Max(shown[len(shown)-1])
	}
	return shown, next
}

// threads returns the open folder's threads as lists of UIDs, newest conversation first.
func (c *Conn) threads() ([][]imap.UID, error) {
	all := &imap.SearchCriteria{}
	if c.Caps().Has("THREAD=REFS") {
		// REFS orders threads by their latest message, oldest first.
		data, err := c.UIDThread(&imapclient.ThreadOptions{Algorithm: "REFS", SearchCriteria: all}).Wait()
		if err != nil {
			return nil, err
		}
		threads := flatten(data)
		slices.Reverse(threads)
		return threads, nil
	}

	// Elsewhere: REFERENCES groups the messages, and a sort by arrival orders the groups.
	data, err := c.UIDThread(&imapclient.ThreadOptions{Algorithm: imap.ThreadReferences, SearchCriteria: all}).Wait()
	if err != nil {
		return nil, err
	}
	sorted, err := c.UIDSort(&imapclient.SortOptions{
		SearchCriteria: all,
		SortCriteria:   []imapclient.SortCriterion{{Key: imapclient.SortKeyArrival, Reverse: true}},
	}).Wait()
	if err != nil {
		return nil, err
	}
	rank := make(map[imap.UID]int, len(sorted))
	for i, uid := range sorted {
		rank[imap.UID(uid)] = i
	}
	threads := flatten(data)
	newest := func(t []imap.UID) int {
		best := len(sorted)
		for _, uid := range t {
			if r, ok := rank[uid]; ok && r < best {
				best = r
			}
		}
		return best
	}
	slices.SortStableFunc(threads, func(a, b []imap.UID) int { return newest(a) - newest(b) })
	return threads, nil
}

func flatten(data []imapclient.ThreadData) [][]imap.UID {
	threads := make([][]imap.UID, 0, len(data))
	var walk func(t imapclient.ThreadData, into []imap.UID) []imap.UID
	walk = func(t imapclient.ThreadData, into []imap.UID) []imap.UID {
		for _, n := range t.Chain {
			into = append(into, imap.UID(n))
		}
		for _, sub := range t.SubThreads {
			into = walk(sub, into)
		}
		return into
	}
	for _, t := range data {
		if uids := walk(t, nil); len(uids) > 0 {
			threads = append(threads, uids)
		}
	}
	return threads
}

// summarise fetches what the list shows for each thread, in one round trip plus one for the
// snippets.
func (c *Conn) summarise(threads [][]imap.UID) ([]Conversation, error) {
	var set imap.UIDSet
	for _, t := range threads {
		set.AddNum(t...)
	}
	msgs, err := c.Fetch(set, &imap.FetchOptions{
		UID: true, Flags: true, Envelope: true, InternalDate: true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}).Collect()
	if err != nil {
		return nil, err
	}
	byUID := make(map[imap.UID]*imapclient.FetchMessageBuffer, len(msgs))
	for _, m := range msgs {
		byUID[m.UID] = m
	}

	convs := make([]Conversation, 0, len(threads))
	latest := make([]*imapclient.FetchMessageBuffer, 0, len(threads))
	for _, t := range threads {
		var found []*imapclient.FetchMessageBuffer
		for _, uid := range t {
			if m := byUID[uid]; m != nil {
				found = append(found, m)
			}
		}
		if len(found) == 0 {
			continue // expunged since the thread was computed
		}
		// Oldest first by arrival, then UID.
		slices.SortFunc(found, func(a, b *imapclient.FetchMessageBuffer) int {
			if c := a.InternalDate.Compare(b.InternalDate); c != 0 {
				return c
			}
			return int(a.UID) - int(b.UID)
		})
		conv := Conversation{ID: slices.Min(t), Count: len(found), Participants: []Person{}}
		for _, m := range found {
			conv.UIDs = append(conv.UIDs, m.UID)
			if !slices.Contains(m.Flags, imap.FlagSeen) {
				conv.Unread = true
			}
			if slices.Contains(m.Flags, imap.FlagFlagged) {
				conv.Flagged = true
			}
			if hasAttachment(m.BodyStructure) {
				conv.Attachments = true
			}
			if m.Envelope != nil {
				for _, a := range m.Envelope.From {
					p := Person{Name: a.Name, Email: strings.ToLower(a.Addr())}
					if !slices.ContainsFunc(conv.Participants, func(q Person) bool { return q.Email == p.Email }) {
						conv.Participants = append(conv.Participants, p)
					}
				}
			}
		}
		first, last := found[0], found[len(found)-1]
		if first.Envelope != nil {
			conv.Subject = first.Envelope.Subject
		}
		conv.Date = last.InternalDate
		convs = append(convs, conv)
		latest = append(latest, last)
	}

	snippets, err := c.snippets(latest)
	if err != nil {
		return nil, err
	}
	for i := range convs {
		convs[i].Snippet = snippets[latest[i].UID]
	}
	return convs, nil
}

func hasAttachment(bs imap.BodyStructure) bool {
	found := false
	if bs == nil {
		return false
	}
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		single, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		d := single.Disposition()
		if d != nil && strings.EqualFold(d.Value, "attachment") {
			found = true
		} else if single.Filename() != "" && (d == nil || !strings.EqualFold(d.Value, "inline")) && single.Type != "text" {
			found = true
		}
		return !found
	})
	return found
}
