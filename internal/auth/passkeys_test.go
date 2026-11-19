package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kspears/postbox/internal/secret"
	"github.com/kspears/postbox/internal/store"
)

func TestCeremonies(t *testing.T) {
	var c ceremonies
	now := time.Unix(1_800_000_000, 0)
	nonce := c.put(now, ceremony{account: "a"})
	if _, err := c.take(now.Add(ceremonyLife), nonce, true); !errors.Is(err, ErrExpired) {
		t.Errorf("after its life: %v", err)
	}
	nonce = c.put(now, ceremony{account: "a"})
	if cer, err := c.take(now, nonce, true); err != nil || cer.account != "a" {
		t.Fatalf("kept take: %+v, %v", cer, err)
	}
	if _, err := c.take(now, nonce, false); err != nil {
		t.Fatalf("second take: %v", err)
	}
	if _, err := c.take(now, nonce, false); !errors.Is(err, ErrExpired) {
		t.Errorf("used nonce: %v", err)
	}
}

func TestLockKey(t *testing.T) {
	a := &Authenticator{}
	prf := make([]byte, 32)
	k1, err := a.lockKey(store.LockPRF, prf, []byte("one"))
	if err != nil || len(k1) != 32 {
		t.Fatalf("key: %x, %v", k1, err)
	}
	k2, _ := a.lockKey(store.LockPRF, prf, []byte("two"))
	if string(k1) == string(k2) {
		t.Error("two passkeys with one PRF output share a key")
	}
	sealed, _ := secret.Seal(k1, bound([]byte("one"), "a@example.test"), "pw")
	if _, err := secret.Open(k1, bound([]byte("one"), "b@example.test"), sealed); err == nil {
		t.Error("a sealed password opened for another account")
	}
	if _, err := a.lockKey(store.LockPRF, nil, []byte("one")); !errors.Is(err, ErrNoPRF) {
		t.Errorf("no PRF output: %v", err)
	}

	// A passkey without PRF needs POSTBOX_PASSKEY_KEY, and gets a key unlike a PRF one's.
	if _, err := a.lockKey(store.LockServer, nil, []byte("one")); !errors.Is(err, ErrNoPRF) {
		t.Errorf("no server key: %v", err)
	}
	a.serverKey = prf
	k3, err := a.lockKey(store.LockServer, nil, []byte("one"))
	if err != nil || string(k3) == string(k1) {
		t.Errorf("server key: %x, %v", k3, err)
	}
}

// A passkey sign-in that found its stored password refused takes the new one, checked like a
// password sign-in, and seals it for the passkey.
func TestNewPassword(t *testing.T) {
	a, _, _ := newFakeAuth(t)
	ctx := context.Background()
	const account = "a@example.test"
	id := []byte("credential")
	key := make([]byte, 32)
	old, _ := secret.Seal(key, bound(id, account), "old")
	p := store.Passkey{ID: id, Account: account, UserHandle: []byte("h"), Credential: []byte(`{}`), Sealed: old, Lock: store.LockPRF, CreatedAt: a.clock.Now()}
	if err := a.store.AddPasskey(ctx, p); err != nil {
		t.Fatal(err)
	}
	nonce := a.pending.put(a.clock.Now(), ceremony{account: account, passkey: p, key: key})

	if _, err := a.NewPassword(ctx, client, nonce, "wrong"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	got, err := a.NewPassword(ctx, client, nonce, rightPassword)
	if err != nil || got != account {
		t.Fatalf("right password: %q, %v", got, err)
	}
	saved, _ := a.store.PasskeyByID(ctx, id)
	if pw, err := secret.Open(key, bound(id, account), saved.Sealed); err != nil || pw != rightPassword {
		t.Errorf("stored password %q, %v", pw, err)
	}
	if _, err := a.NewPassword(ctx, client, nonce, rightPassword); !errors.Is(err, ErrExpired) {
		t.Errorf("nonce reused: %v", err)
	}
	// A sign-in or enrolment nonce is not a new-password one.
	if _, err := a.NewPassword(ctx, client, a.pending.put(a.clock.Now(), ceremony{account: account}), rightPassword); !errors.Is(err, ErrExpired) {
		t.Errorf("enrolment nonce: %v", err)
	}
}

func TestSignInOptions(t *testing.T) {
	a, _, _ := newFakeAuth(t)
	nonce, options, err := a.SignInOptions()
	if err != nil || nonce == "" {
		t.Fatal(err)
	}
	if options.Response.RelyingPartyID != "mail.example.test" || options.Response.Extensions.PRF == nil {
		t.Errorf("options: %+v", options.Response)
	}
	// Garbage for an answer fails, and counts against the throttle.
	if _, _, err := a.SignIn(context.Background(), client, nonce, []byte(`{}`), nil); !errors.Is(err, ErrUnknownPasskey) {
		t.Errorf("garbage: %v", err)
	}
	if _, _, err := a.SignIn(context.Background(), client, nonce, []byte(`{}`), nil); !errors.Is(err, ErrExpired) {
		t.Errorf("reused nonce: %v", err)
	}
}

func TestEnrolOptions(t *testing.T) {
	a, _, _ := newFakeAuth(t)
	nonce, options, err := a.EnrolOptions(context.Background(), "a@example.test")
	if err != nil || nonce == "" {
		t.Fatal(err)
	}
	sel := options.Response.AuthenticatorSelection
	if sel.ResidentKey != "required" || sel.UserVerification != "required" || options.Response.Extensions.PRF == nil {
		t.Errorf("options: %+v", options.Response)
	}
	if _, err := a.Enrol(context.Background(), "b@example.test", "pw", nonce, nil, nil); !errors.Is(err, ErrExpired) {
		t.Errorf("another account's nonce: %v", err)
	}
}
