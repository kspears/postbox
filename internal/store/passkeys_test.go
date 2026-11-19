package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPasskeys(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	const account = "a@example.test"
	now := time.Unix(1_800_000_000, 0)

	if err := s.RequirePasskey(ctx, account, true, ""); !errors.Is(err, ErrNoPasskey) {
		t.Fatalf("require with no passkey: %v", err)
	}
	for i, id := range []string{"one", "two"} {
		err := s.AddPasskey(ctx, Passkey{
			ID: []byte(id), Account: account, UserHandle: []byte("h"), Credential: []byte(`{}`),
			Sealed: []byte("s"), Lock: LockPRF, CreatedAt: now.Add(time.Duration(i) * time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.Passkeys(ctx, account)
	if err != nil || len(list) != 2 || string(list[0].ID) != "one" {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if err := s.UpdatePasskeyCredential(ctx, []byte("one"), []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePasskeySealed(ctx, []byte("one"), []byte("new")); err != nil {
		t.Fatal(err)
	}
	if p, err := s.PasskeyByID(ctx, []byte("one")); err != nil || string(p.Sealed) != "new" || string(p.Credential) != `{"n":1}` || p.Lock != LockPRF {
		t.Errorf("after update: %+v, %v", p, err)
	}

	// Turning it on ends password sessions other than the one asking.
	for _, ses := range []Session{
		{ID: "mine", Method: "password"}, {ID: "other", Method: "password"}, {ID: "key", Method: "passkey"},
	} {
		ses.Account, ses.Sealed, ses.CreatedAt, ses.ExpiresAt = account, []byte("x"), now, now.Add(time.Hour)
		if err := s.CreateSession(ctx, ses); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RequirePasskey(ctx, account, true, "mine"); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"mine": true, "other": false, "key": true} {
		if _, err := s.Session(ctx, id); (err == nil) != want {
			t.Errorf("session %s kept = %v, want %v", id, err == nil, want)
		}
	}

	// Removing a passkey keeps the switch on; removing the last turns it off.
	// Removing a passkey ends the other passkey sessions.
	if err := s.CreateSession(ctx, Session{ID: "key2", Account: account, Sealed: []byte("x"), Method: MethodPasskey, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if on, err := s.RemovePasskey(ctx, account, []byte("one"), "key"); err != nil || !on {
		t.Errorf("remove one: %v, %v", on, err)
	}
	for id, want := range map[string]bool{"key": true, "key2": false, "mine": true} {
		if _, err := s.Session(ctx, id); (err == nil) != want {
			t.Errorf("after removal, session %s kept = %v, want %v", id, err == nil, want)
		}
	}
	if _, err := s.RemovePasskey(ctx, "b@example.test", []byte("two"), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account's passkey: %v", err)
	}
	if on, err := s.RemovePasskey(ctx, account, []byte("two"), ""); err != nil || on {
		t.Errorf("remove last: %v, %v", on, err)
	}

	if err := s.AddPasskey(ctx, Passkey{ID: []byte("three"), Account: account, UserHandle: []byte("h"), Credential: []byte(`{}`), Sealed: []byte("s"), Lock: LockServer, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.RequirePasskey(ctx, account, true, ""); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ClearPasskeys(ctx, account); err != nil || n != 1 {
		t.Errorf("clear: %d, %v", n, err)
	}
	if on, _ := s.PasskeyRequired(ctx, account); on {
		t.Error("require still on after clear")
	}
}
