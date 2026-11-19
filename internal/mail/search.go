package mail

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Found is one message a search found: a conversation of that message alone, and its folder.
type Found struct {
	Folder string `json:"folder"`
	Conversation
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
// searches every folder but those two and their subfolders (spec, "Search scope").
func (p *Pool) Search(ctx context.Context, acct Account, from, query string, offset int) (*Results, error) {
	criteria := searchCriteria(query)
	out := &Results{Messages: []Found{}}
	if criteria == nil {
		return out, nil
	}
	offset = max(offset, 0)
	var folders []string
	err := p.Do(ctx, acct, func(c *Conn) (err error) {
		folders, err = c.searchScope(from)
		return err
	})
	if err != nil {
		return nil, err
	}

	// Each folder's newest offset+PageSize matches are enough to make the merged page.
	var total atomic.Int64
	hits, err := eachFolder(folders, func(folder string) ([]hit, error) {
		var found []hit
		err := p.Do(ctx, acct, func(c *Conn) error {
			var n int
			var err error
			found, n, err = c.searchFolder(folder, criteria, offset+PageSize)
			total.Add(int64(n))
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
	page := all[min(offset, len(all)):min(offset+PageSize, len(all))]
	if end := offset + len(page); int64(end) < total.Load() {
		out.Next = end
	}

	// Summaries for the page, by folder; then back into the order found.
	byFolder := map[string][]imap.UID{}
	for _, h := range page {
		byFolder[h.folder] = append(byFolder[h.folder], h.uid)
	}
	pageFolders := slices.Collect(maps.Keys(byFolder))
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
	summary := map[hit]Found{}
	for _, f := range slices.Concat(summaries...) {
		summary[hit{folder: f.Folder, uid: f.ID}] = f
	}
	for _, h := range page {
		if f, ok := summary[hit{folder: h.folder, uid: h.uid}]; ok {
			out.Messages = append(out.Messages, f)
		}
	}
	return out, nil
}

// eachFolder runs fn for every folder, one per command connection at a time, and returns the
// answers in folders' order, or the first error.
func eachFolder[T any](folders []string, fn func(string) ([]T, error)) ([][]T, error) {
	out := make([][]T, len(folders))
	errs := make([]error, len(folders))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(commandConns, len(folders)) {
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

// searchScope returns the folders a search started from folder covers: Junk or Trash, with
// their subfolders, when it starts there; otherwise every other folder.
func (c *Conn) searchScope(from string) ([]string, error) {
	folders, delim, err := c.folderTree()
	if err != nil {
		return nil, err
	}
	return scope(folders, delim, from), nil
}

func scope(folders []Folder, delim, from string) []string {
	var bins []string
	for _, f := range folders {
		if f.Role == RoleJunk || f.Role == RoleTrash {
			bins = append(bins, f.Name)
		}
	}
	in := func(name string) bool {
		return slices.ContainsFunc(bins, func(bin string) bool { return Within(name, bin, delim) })
	}
	bin := slices.IndexFunc(bins, func(b string) bool { return Within(from, b, delim) })
	var out []string
	for _, f := range folders {
		if !f.Selectable {
			continue
		}
		if bin >= 0 && Within(f.Name, bins[bin], delim) || bin < 0 && !in(f.Name) {
			out = append(out, f.Name)
		}
	}
	return out
}

// searchFolder returns the newest limit messages in folder that match criteria, with their
// arrival dates, and how many match in all. A folder that will not open has none.
func (c *Conn) searchFolder(folder string, criteria *imap.SearchCriteria, limit int) ([]hit, int, error) {
	box, err := c.Client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		if isClientNo(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	if box.NumMessages == 0 {
		return nil, 0, nil
	}
	sorted, err := c.UIDSort(&imapclient.SortOptions{
		SearchCriteria: criteria,
		SortCriteria:   []imapclient.SortCriterion{{Key: imapclient.SortKeyArrival, Reverse: true}},
	}).Wait()
	if err != nil {
		return nil, 0, err
	}
	if len(sorted) == 0 {
		return nil, 0, nil
	}
	newest := make([]imap.UID, 0, min(limit, len(sorted)))
	for _, uid := range sorted[:min(limit, len(sorted))] {
		newest = append(newest, imap.UID(uid))
	}
	msgs, err := c.Fetch(imap.UIDSetNum(newest...), &imap.FetchOptions{UID: true, InternalDate: true}).Collect()
	if err != nil {
		return nil, 0, err
	}
	hits := make([]hit, 0, len(msgs))
	for _, m := range msgs {
		hits = append(hits, hit{folder: folder, uid: m.UID, date: m.InternalDate})
	}
	return hits, len(sorted), nil
}

// found fetches what the results list shows for uids in folder: each as a conversation of one.
func (c *Conn) found(folder string, uids []imap.UID) ([]Found, error) {
	if _, err := c.Client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, err
	}
	threads := make([][]imap.UID, len(uids))
	for i, uid := range uids {
		threads[i] = []imap.UID{uid}
	}
	convs, err := c.summarise(threads)
	if err != nil {
		return nil, err
	}
	out := make([]Found, len(convs))
	for i, conv := range convs {
		out[i] = Found{Folder: folder, Conversation: conv}
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
