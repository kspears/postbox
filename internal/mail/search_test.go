package mail

import (
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestSearchWords(t *testing.T) {
	for in, want := range map[string][]string{
		"":                      nil,
		"   ":                   nil,
		"ladder":                {"ladder"},
		"  bring  the\tladder ": {"bring", "the", "ladder"},
		`"the ladder" bring`:    {"the ladder", "bring"},
		`unclosed "quote here`:  {"unclosed", "quote here"},
		"line\r\nbreak":         {"line", "break"},
		"null\x00byte":          {"nullbyte"},
	} {
		if got := searchWords(in); !slices.Equal(got, want) {
			t.Errorf("searchWords(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchCriteria(t *testing.T) {
	if searchCriteria("  ") != nil {
		t.Error("an empty query searched")
	}
	c := searchCriteria("bring ladder")
	// One OR group per word, each covering the body and every searched header.
	if len(c.Or) != 2 {
		t.Fatalf("%d word groups, want 2", len(c.Or))
	}
	var fields []string
	var body []string
	stack := []imap.SearchCriteria{c.Or[0][0], c.Or[0][1]}
	for len(stack) > 0 {
		sc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, h := range sc.Header {
			fields = append(fields, h.Key)
		}
		body = append(body, sc.Body...)
		for _, or := range sc.Or {
			stack = append(stack, or[0], or[1])
		}
	}
	slices.Sort(fields)
	if !slices.Equal(fields, []string{"Bcc", "Cc", "From", "Subject", "To"}) || !slices.Equal(body, []string{"bring"}) {
		t.Errorf("first word searches headers %v and body %v", fields, body)
	}
}

func TestEachFolder(t *testing.T) {
	var running, most atomic.Int32
	got, err := eachFolder([]string{"a", "b", "c", "d", "e"}, func(f string) ([]string, error) {
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		defer running.Add(-1)
		return []string{f + "1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(slices.Concat(got...), []string{"a1", "b1", "c1", "d1", "e1"}) {
		t.Errorf("answers %v are not in folder order", got)
	}
	if most.Load() > searchParallel {
		t.Errorf("%d folders searched at once, limit %d", most.Load(), searchParallel)
	}

	boom := errors.New("boom")
	if _, err := eachFolder([]string{"a", "b"}, func(f string) ([]int, error) {
		if f == "b" {
			return nil, boom
		}
		return nil, nil
	}); !errors.Is(err, boom) {
		t.Errorf("error %v, want boom", err)
	}
	if got, err := eachFolder(nil, func(string) ([]int, error) { return nil, nil }); err != nil || len(got) != 0 {
		t.Errorf("no folders: %v, %v", got, err)
	}
}
