package mail

import (
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"
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
		want   []imap.UID
		next   imap.UID
	}{
		{"first page", 0, []imap.UID{10, 8}, 8},
		{"after a cursor", 8, []imap.UID{7, 6}, 6},
		{"last page", 5, []imap.UID{4}, 0},
		{"cursor's thread moved up", 9, []imap.UID{8, 7}, 7},
		{"cursor below everything", 2, nil, 0},
	}
	for _, tt := range tests {
		shown, next := pageOf(threads, tt.cursor, 2)
		if got := latest(shown); !slices.Equal(got, tt.want) || next != tt.next {
			t.Errorf("%s: got %v next %d, want %v next %d", tt.name, got, next, tt.want, tt.next)
		}
	}
}

func TestFlatten(t *testing.T) {
	got := flatten(nil)
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}
