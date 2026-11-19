package store

import (
	"context"
	"errors"
	"testing"
)

func TestIdentities(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	list, err := s.Identities(ctx, "a@example.test")
	if err != nil || len(list) != 1 || list[0].Email != "a@example.test" {
		t.Fatalf("first read: %+v, %v", list, err)
	}
	login := list[0]

	added, err := s.SaveIdentity(ctx, "a@example.test", Identity{Name: "Sales", Email: "sales@example.test", Signature: "<p>Sales</p>"})
	if err != nil || added.ID == 0 {
		t.Fatalf("add: %+v, %v", added, err)
	}
	login.Name, login.Signature = "Alice", "<p>A</p>"
	if _, err := s.SaveIdentity(ctx, "a@example.test", login); err != nil {
		t.Fatal(err)
	}
	login.Email = "other@example.test"
	if _, err := s.SaveIdentity(ctx, "a@example.test", login); !errors.Is(err, ErrLoginIdentity) {
		t.Errorf("changing the login address: %v", err)
	}
	if err := s.DeleteIdentity(ctx, "a@example.test", login.ID); !errors.Is(err, ErrLoginIdentity) {
		t.Errorf("deleting the login identity: %v", err)
	}
	if err := s.DeleteIdentity(ctx, "b@example.test", added.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account deleted it: %v", err)
	}

	list, _ = s.Identities(ctx, "a@example.test")
	if len(list) != 2 || list[0].Name != "Alice" || list[1].Email != "sales@example.test" {
		t.Errorf("got %+v", list)
	}
	if err := s.DeleteIdentity(ctx, "a@example.test", added.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.Identities(ctx, "a@example.test"); len(list) != 1 {
		t.Errorf("after delete: %+v", list)
	}
}
