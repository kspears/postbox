package mail

import (
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestPageOf(t *testing.T) {
	// Newest first; each thread's latest UID is its largest.
	threads := [][]imap.UID{{9, 10}, {8}, {3, 7}, {6}, {1, 5}, {4}}
	latest := func(page [][]imap.UID) []imap.UID {
		var l []imap.UID
		for _, th := range page {
			l = append(l, slices.Max(th))
		}
		return l
	}
	tests := []struct {
		name   string
		cursor imap.UID
		shown  int
		want   []imap.UID
		next   imap.UID
	}{
		{"first page", 0, 0, []imap.UID{10, 8}, 8},
		{"after a cursor", 8, 2, []imap.UID{7, 6}, 6},
		{"last page", 5, 5, []imap.UID{4}, 0},
		{"cursor's thread had a reply: continue by position", 9, 2, []imap.UID{7, 6}, 6},
		{"cursor's thread is gone, shown past the end", 2, 9, nil, 0},
	}
	for _, tt := range tests {
		shown, next := pageOf(threads, tt.cursor, tt.shown, 2)
		if got := latest(shown); !slices.Equal(got, tt.want) || next != tt.next {
			t.Errorf("%s: got %v next %d, want %v next %d", tt.name, got, next, tt.want, tt.next)
		}
	}
}

func TestFlatten(t *testing.T) {
	data := []imapclient.ThreadData{
		{Chain: []uint32{1, 2}, SubThreads: []imapclient.ThreadData{{Chain: []uint32{3}}, {Chain: []uint32{4, 5}}}},
		{Chain: []uint32{6}},
		{}, // a thread of messages that are all gone
	}
	got := flatten(data)
	if len(got) != 2 || !slices.Equal(got[0], []imap.UID{1, 2, 3, 4, 5}) || !slices.Equal(got[1], []imap.UID{6}) {
		t.Errorf("got %v", got)
	}
}
