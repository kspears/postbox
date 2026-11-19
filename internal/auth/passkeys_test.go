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
	put := func(cer ceremony) string {
		t.Helper()
		nonce, err := c.put(now, cer)
		if err != nil {
			t.Fatal(err)
		}
		return nonce
	}
	nonce := put(ceremony{kind: signIn})
	if _, err := c.take(now.Add(ceremonyLife), nonce, signIn); !errors.Is(err, ErrExpired) {
		t.Errorf("after its life: %v", err)
	}
	nonce = put(ceremony{kind: newPassword, account: "a"})
	if cer, err := c.get(now, nonce, newPassword); err != nil || cer.account != "a" {
		t.Fatalf("get: %+v, %v", cer, err)
	}
	// A nonce finishes only its own kind of ceremony.
	for _, kind := range []ceremonyKind{enrolment, signIn} {
		if _, err := c.take(now, nonce, kind); !errors.Is(err, ErrExpired) {
			t.Errorf("as kind %d: %v", kind, err)
		}
	}
	if _, err := c.take(now, nonce, newPassword); err != nil {
		t.Fatalf("take: %v", err)
	}
	if _, err := c.take(now, nonce, newPassword); !errors.Is(err, ErrExpired) {
		t.Errorf("used nonce: %v", err)
	}

	// Full of live ceremonies, the next is refused; once they expire, there is room again.
	for range maxCeremonies - len(c.byID) {
		put(ceremony{kind: signIn})
	}
	if _, err := c.put(now, ceremony{kind: signIn}); !errors.Is(err, ErrBusy) {
		t.Errorf("full: %v", err)
	}
	if _, err := c.put(now.Add(ceremonyLife), ceremony{kind: signIn}); err != nil || len(c.byID) != 1 {
		t.Errorf("after expiry: %v, %d held", err, len(c.byID))
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
	nonce, _ := a.pending.put(a.clock.Now(), ceremony{kind: newPassword, account: account, passkeyID: id, key: key})

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
	// An enrolment nonce is not a new-password one.
	enrol, _ := a.pending.put(a.clock.Now(), ceremony{kind: enrolment, account: account})
	if _, err := a.NewPassword(ctx, client, enrol, rightPassword); !errors.Is(err, ErrExpired) {
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

func TestPasswordSignInRequiresPasskey(t *testing.T) {
	a, _, _ := newFakeAuth(t)
	ctx := context.Background()
	const account = "a@example.test"
	if err := a.store.AddPasskey(ctx, store.Passkey{ID: []byte("k"), Account: account, UserHandle: []byte("h"), Credential: []byte(`{}`), Sealed: []byte("s"), Lock: store.LockPRF, CreatedAt: a.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.RequirePasskey(ctx, account, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.PasswordSignIn(ctx, client, account, "wrong"); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong password: %v", err)
	}
	if err := a.PasswordSignIn(ctx, client, account, rightPassword); !errors.Is(err, ErrPasskeyRequired) {
		t.Errorf("right password: %v", err)
	}
	// A passkey's new password is not a password sign-in.
	if err := a.CheckPassword(ctx, client, account, rightPassword); err != nil {
		t.Errorf("check: %v", err)
	}
}

// Failures of passkeys Postbox does not know count against the IP's limit, not against a shared
// account: five of them do not block a password sign-in from the same address.
func TestUnknownPasskeysDoNotBlockTheIP(t *testing.T) {
	a, _, _ := newFakeAuth(t)
	ctx := context.Background()
	for range maxPerAccountIP {
		nonce, _, err := a.SignInOptions()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.SignIn(ctx, client, nonce, []byte(`{}`), nil); !errors.Is(err, ErrUnknownPasskey) {
			t.Fatalf("garbage: %v", err)
		}
	}
	if err := a.CheckPassword(ctx, client, "a@example.test", rightPassword); err != nil {
		t.Errorf("password sign-in after unknown passkeys: %v", err)
	}
}
