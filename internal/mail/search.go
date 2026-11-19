package mail

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/emersion/go-imap/v2"
)

// searchParallel is how many folders are searched at once: the account's command connections.
const searchParallel = commandConns

// Found is one message a search found, as the results list shows it.
type Found struct {
	Folder      string    `json:"folder"`
	UID         imap.UID  `json:"uid"`
	Subject     string    `json:"subject"`
	From        []Person  `json:"from"`
	Snippet     string    `json:"snippet"`
	Date        time.Time `json:"date"`
	Unread      bool      `json:"unread"`
	Flagged     bool      `json:"flagged"`
	Attachments bool      `json:"attachments"`
}

// Results is one page of a search. Next is the offset of the page after it, or 0 at the end.
type Results struct {
	Messages []Found `json:"messages"`
	Next     int     `json:"next,omitempty"`
}

// hit is a matching message before its summary is fetched.
type hit struct {
	folder string
	uid    imap.UID
	date   time.Time
}

// Search finds the messages matching query, newest first, and returns the page of PageSize
// starting at offset. From inside Junk or Trash it searches that folder only; elsewhere it
// searches every folder but those two (spec, "Search scope").
func (p *Pool) Search(ctx context.Context, acct Account, from, query string, offset int) (*Results, error) {
	criteria := searchCriteria(query)
	out := &Results{Messages: []Found{}}
	if criteria == nil {
		return out, nil
	}
	var folders []string
	err := p.Do(ctx, acct, func(c *Conn) (err error) {
		folders, err = c.searchScope(from)
		return err
	})
	if err != nil {
		return nil, err
	}

	hits, err := eachFolder(folders, func(folder string) ([]hit, error) {
		var found []hit
		err := p.Do(ctx, acct, func(c *Conn) (err error) {
			found, err = c.searchFolder(folder, criteria)
			return err
		})
		return found, err
	})
	if err != nil {
		return nil, err
	}
	all := slices.Concat(hits...)
	slices.SortFunc(all, func(a, b hit) int {
		return cmp.Or(b.date.Compare(a.date), cmp.Compare(a.folder, b.folder), cmp.Compare(b.uid, a.uid))
	})
	offset = min(max(offset, 0), len(all))
	end := min(offset+PageSize, len(all))
	page := all[offset:end]
	if end < len(all) {
		out.Next = end
	}

	// Summaries for the page, by folder; then back into the order found.
	byFolder := map[string][]imap.UID{}
	for _, h := range page {
		byFolder[h.folder] = append(byFolder[h.folder], h.uid)
	}
	pageFolders := make([]string, 0, len(byFolder))
	for f := range byFolder {
		pageFolders = append(pageFolders, f)
	}
	summaries, err := eachFolder(pageFolders, func(folder string) ([]Found, error) {
		var found []Found
		err := p.Do(ctx, acct, func(c *Conn) (err error) {
			found, err = c.found(folder, byFolder[folder])
			return err
		})
		return found, err
	})
	if err != nil {
		return nil, err
	}
	type key struct {
		folder string
		uid    imap.UID
	}
	summary := map[key]Found{}
	for _, list := range summaries {
		for _, f := range list {
			summary[key{f.Folder, f.UID}] = f
		}
	}
	for _, h := range page {
		if f, ok := summary[key{h.folder, h.uid}]; ok {
			out.Messages = append(out.Messages, f)
		}
	}
	return out, nil
}

// eachFolder runs fn for every folder, searchParallel at a time, and returns the answers in
// folders' order, or the first error.
func eachFolder[T any](folders []string, fn func(string) ([]T, error)) ([][]T, error) {
	out := make([][]T, len(folders))
	errs := make([]error, len(folders))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(searchParallel, len(folders)) {
		wg.Go(func() {
			for i := range next {
				out[i], errs[i] = fn(folders[i])
			}
		})
	}
	for i := range folders {
		next <- i
	}
	close(next)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// searchScope returns the folders a search started from folder covers.
func (c *Conn) searchScope(from string) ([]string, error) {
	folders, _, err := c.folderTree()
	if err != nil {
		return nil, err
	}
	for _, f := range folders {
		if f.Name == from && (f.Role == RoleJunk || f.Role == RoleTrash) {
			return []string{from}, nil
		}
	}
	var out []string
	for _, f := range folders {
		if f.Selectable && f.Role != RoleJunk && f.Role != RoleTrash {
			out = append(out, f.Name)
		}
	}
	return out, nil
}

// searchFolder returns the messages in folder that match criteria, with their arrival dates.
// A folder that will not open has none.
func (c *Conn) searchFolder(folder string, criteria *imap.SearchCriteria) ([]hit, error) {
	box, err := c.Client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		if isClientNo(err) {
			return nil, nil
		}
		return nil, err
	}
	if box.NumMessages == 0 {
		return nil, nil
	}
	data, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, err
	}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, InternalDate: true}).Collect()
	if err != nil {
		return nil, err
	}
	hits := make([]hit, 0, len(msgs))
	for _, m := range msgs {
		hits = append(hits, hit{folder: folder, uid: m.UID, date: m.InternalDate})
	}
	return hits, nil
}

// found fetches what the results list shows for uids in folder.
func (c *Conn) found(folder string, uids []imap.UID) ([]Found, error) {
	if _, err := c.Client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, err
	}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
		UID: true, Flags: true, Envelope: true, InternalDate: true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}).Collect()
	if err != nil {
		return nil, err
	}
	snippets, err := c.snippets(msgs)
	if err != nil {
		return nil, err
	}
	out := make([]Found, 0, len(msgs))
	for _, m := range msgs {
		f := Found{
			Folder:      folder,
			UID:         m.UID,
			From:        []Person{},
			Snippet:     snippets[m.UID],
			Date:        m.InternalDate,
			Unread:      !slices.Contains(m.Flags, imap.FlagSeen),
			Flagged:     slices.Contains(m.Flags, imap.FlagFlagged),
			Attachments: hasAttachment(m.BodyStructure),
		}
		if m.Envelope != nil {
			f.Subject = m.Envelope.Subject
			f.From = people(m.Envelope.From)
		}
		out = append(out, f)
	}
	return out, nil
}

// searchFields are where a word may appear: the sender, the recipients, the subject and the
// body.
var searchFields = []string{"From", "To", "Cc", "Bcc", "Subject"}

// searchCriteria matches messages that contain every word of query, each in any of the
// searched fields; a phrase in double quotes counts as one word. It is nil for an empty query.
func searchCriteria(query string) *imap.SearchCriteria {
	words := searchWords(query)
	if len(words) == 0 {
		return nil
	}
	all := &imap.SearchCriteria{}
	for _, w := range words {
		anywhere := imap.SearchCriteria{Body: []string{w}}
		for _, field := range searchFields {
			one := imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: field, Value: w}}}
			anywhere = imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{anywhere, one}}}
		}
		all.And(&anywhere)
	}
	return all
}

// searchWords splits query on spaces outside double quotes.
func searchWords(query string) []string {
	var words []string
	var word strings.Builder
	quoted := false
	flush := func() {
		if w := strings.TrimSpace(word.String()); w != "" {
			words = append(words, w)
		}
		word.Reset()
	}
	for _, r := range query {
		switch {
		case r == '"':
			flush()
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		case unicode.IsControl(r):
		default:
			word.WriteRune(r)
		}
	}
	flush()
	return words
}
