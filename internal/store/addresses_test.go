package store

import (
	"context"
	"testing"
	"time"
)

func TestCollectedAddressesMatchNameOrEmailPrefix(t *testing.T) {
	s, ctx := open(t), context.Background()
	at := time.Unix(1_800_000_000, 0)
	people := []Address{{Name: "Carol Jones", Email: "Carol@Example.test"}, {Email: "bob@example.test"}, {Email: "not an address"}, {Email: ""}}
	if err := s.CollectAddresses(ctx, "a", people, at); err != nil {
		t.Fatal(err)
	}
	for prefix, want := range map[string]string{"car": "carol@example.test", "JON": "", "Carol J": "carol@example.test", "bob@": "bob@example.test", "not": ""} {
		got, err := s.MatchAddresses(ctx, "a", prefix, 8)
		if err != nil {
			t.Fatal(err)
		}
		if want == "" && len(got) != 0 || want != "" && (len(got) != 1 || got[0].Email != want) {
			t.Errorf("%q: got %+v, want %q", prefix, got, want)
		}
	}
	if got, _ := s.MatchAddresses(ctx, "b", "car", 8); len(got) != 0 {
		t.Errorf("another account sees %+v", got)
	}
	if got, _ := s.MatchAddresses(ctx, "a", "", 8); got == nil || len(got) != 0 {
		t.Errorf("empty prefix returned %#v", got)
	}
}

func TestCollectedAddressesRankAndKeepNewestName(t *testing.T) {
	s, ctx := open(t), context.Background()
	at := time.Unix(1_800_000_000, 0)
	s.CollectAddresses(ctx, "a", []Address{{Name: "Old", Email: "x1@example.test"}, {Email: "x2@example.test"}}, at)
	s.CollectAddresses(ctx, "a", []Address{{Name: "New", Email: "X1@example.test"}}, at.Add(time.Hour))
	s.CollectAddresses(ctx, "a", []Address{{Email: "x1@example.test"}}, at.Add(2*time.Hour))
	s.CollectAddresses(ctx, "a", []Address{{Name: "Stale", Email: "x1@example.test"}}, at.Add(-time.Hour))
	got, err := s.MatchAddresses(ctx, "a", "x", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Address{Name: "New", Email: "x1@example.test"}) || got[1].Email != "x2@example.test" {
		t.Errorf("got %+v", got)
	}
	if got, _ := s.MatchAddresses(ctx, "a", "x", 1); len(got) != 1 {
		t.Errorf("limit ignored: %+v", got)
	}
}

func TestMatchAddressesTakesWildcardsLiterally(t *testing.T) {
	s, ctx := open(t), context.Background()
	s.CollectAddresses(ctx, "a", []Address{{Email: "a_b@example.test"}, {Email: "axb@example.test"}, {Email: "100%@example.test"}}, time.Now())
	for prefix, want := range map[string]int{"a_": 1, "%": 0, "100%": 1, `\`: 0} {
		if got, _ := s.MatchAddresses(ctx, "a", prefix, 8); len(got) != want {
			t.Errorf("%q: got %+v, want %d", prefix, got, want)
		}
	}
}
