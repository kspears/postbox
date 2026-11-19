package api

import (
	"slices"
	"testing"
)

func TestValidLabel(t *testing.T) {
	for label, want := range map[string]bool{
		"Receipts": true, "Trips 2026": true, "Café": true,
		"": false, " padded": false, "a/b": false, "a.b": false, "100%": false, "star*": false,
		`quote"`: false, "back\\slash": false, "tab\there": false,
	} {
		if got := validLabel(label); got != want {
			t.Errorf("%q: got %v, want %v", label, got, want)
		}
	}
}

func TestRenameInOrder(t *testing.T) {
	order := []string{"INBOX", "Foo", "Foo/Child", "Foo/Child/Deep", "Foo Bar", "Foo.bar", "Other"}
	got := renameInOrder(slices.Clone(order), "Foo", "Projects/Foo", "/")
	want := []string{"INBOX", "Projects/Foo", "Projects/Foo/Child", "Projects/Foo/Child/Deep", "Foo Bar", "Foo.bar", "Other"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
