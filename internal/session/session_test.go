package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kspears/postbox/internal/clock"
	"github.com/kspears/postbox/internal/store"
)

const password = "correct horse battery staple"

func newManager(t *testing.T, secure bool) (*Manager, *clock.Clock, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clk := &clock.Clock{}
	return NewManager(st, clk, secure), clk, dir
}

func request(c *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "/api/session", nil)
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

func TestRoundTrip(t *testing.T) {
	m, _, _ := newManager(t, true)
	c, err := m.Create(context.Background(), "alice@example.test", password, Password, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "__Host-postbox_session" || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie attributes: %+v", c)
	}
	s, err := m.FromRequest(request(c))
	if err != nil {
		t.Fatal(err)
	}
	if s.Account != "alice@example.test" || s.Password != password || s.Method != Password {
		t.Errorf("got %+v", s)
	}
}

func TestInsecureOriginCookie(t *testing.T) {
	m, _, _ := newManager(t, false)
	c, _ := m.Create(context.Background(), "a@example.test", password, Password, false)
	if c.Name != "postbox_session" || c.Secure {
		t.Errorf("cookie for an http://localhost origin: %+v", c)
	}
}

func TestLifetimes(t *testing.T) {
	tests := []struct {
		name   string
		method string
		keep   bool
		maxAge int
		life   time.Duration
	}{
		{"password", Password, false, 0, ShortLife},
		{"password, keep me signed in", Password, true, int(LongLife / time.Second), LongLife},
		{"passkey", Passkey, false, int(LongLife / time.Second), LongLife},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, clk, _ := newManager(t, true)
			c, err := m.Create(context.Background(), "a@example.test", password, tt.method, tt.keep)
			if err != nil {
				t.Fatal(err)
			}
			if c.MaxAge != tt.maxAge || !c.Expires.IsZero() {
				t.Errorf("MaxAge %d, Expires %v; want MaxAge %d and no Expires", c.MaxAge, c.Expires, tt.maxAge)
			}
			clk.Advance(tt.life - time.Second)
			if _, err := m.FromRequest(request(c)); err != nil {
				t.Errorf("a second before expiry: %v", err)
			}
			clk.Advance(time.Second)
			if _, err := m.FromRequest(request(c)); !errors.Is(err, ErrNoSession) {
				t.Errorf("at expiry: got %v, want ErrNoSession", err)
			}
		})
	}
}

func TestRejectsBadCookies(t *testing.T) {
	m, _, _ := newManager(t, true)
	good, _ := m.Create(context.Background(), "a@example.test", password, Password, false)
	id, _, _ := strings.Cut(good.Value, ".")
	otherKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

	for name, value := range map[string]string{
		"empty":       "",
		"no key":      id,
		"short key":   id + ".AAAA",
		"wrong key":   id + "." + otherKey,
		"unknown id":  "nope." + otherKey,
		"not base64":  id + ".***",
		"extra parts": good.Value + ".x",
	} {
		c := *good
		c.Value = value
		if _, err := m.FromRequest(request(&c)); !errors.Is(err, ErrNoSession) {
			t.Errorf("%s: got %v, want ErrNoSession", name, err)
		}
	}
	if _, err := m.FromRequest(request(nil)); !errors.Is(err, ErrNoSession) {
		t.Errorf("no cookie: got %v", err)
	}
}

func TestSealedPasswordIsBoundToItsRow(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	sealed, err := seal(key, "id1", "a@example.test", password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(key, "id2", "a@example.test", sealed); err == nil {
		t.Error("opened under another session id")
	}
	if _, err := open(key, "id1", "b@example.test", sealed); err == nil {
		t.Error("opened under another account")
	}
	if got, err := open(key, "id1", "a@example.test", sealed); err != nil || got != password {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestSignOut(t *testing.T) {
	m, _, _ := newManager(t, true)
	ctx := context.Background()
	a1, _ := m.Create(ctx, "a@example.test", password, Password, false)
	a2, _ := m.Create(ctx, "a@example.test", password, Password, true)
	b, _ := m.Create(ctx, "b@example.test", password, Password, false)

	s1, _ := m.FromRequest(request(a1))
	gone, err := m.End(ctx, s1)
	if err != nil || gone.MaxAge != -1 || gone.Value != "" {
		t.Fatalf("End: %+v, %v", gone, err)
	}
	if _, err := m.FromRequest(request(a1)); !errors.Is(err, ErrNoSession) {
		t.Error("signed-out session still works")
	}
	if _, err := m.FromRequest(request(a2)); err != nil {
		t.Errorf("the account's other session was ended too: %v", err)
	}

	s2, _ := m.FromRequest(request(a2))
	if _, err := m.EndAll(ctx, s2); err != nil {
		t.Fatal(err)
	}
	if _, err := m.FromRequest(request(a2)); !errors.Is(err, ErrNoSession) {
		t.Error("sign out everywhere left a session")
	}
	if _, err := m.FromRequest(request(b)); err != nil {
		t.Errorf("another account was signed out: %v", err)
	}
}

func TestSurvivesReopen(t *testing.T) {
	m, clk, dir := newManager(t, true)
	c, _ := m.Create(context.Background(), "a@example.test", password, Password, true)

	st, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, err := NewManager(st, clk, true).FromRequest(request(c))
	if err != nil || s.Password != password {
		t.Errorf("after reopening the database: %+v, %v", s, err)
	}
}

// Acceptance criterion 4, for sessions: the database file never holds the password in a form
// that can be read back.
func TestDatabaseNeverHoldsThePassword(t *testing.T) {
	m, _, dir := newManager(t, true)
	if _, err := m.Create(context.Background(), "a@example.test", password, Password, true); err != nil {
		t.Fatal(err)
	}
	encodings := map[string]string{
		"plain text": password,
		"base64":     base64.StdEncoding.EncodeToString([]byte(password)),
		"base64url":  base64.RawURLEncoding.EncodeToString([]byte(password)),
		"hex":        hex.EncodeToString([]byte(password)),
	}
	files, _ := filepath.Glob(filepath.Join(dir, "postbox.db*"))
	if len(files) == 0 {
		t.Fatal("no database files")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for name, needle := range encodings {
			if bytes.Contains(bytes.ToLower(data), bytes.ToLower([]byte(needle))) {
				t.Errorf("%s contains the password as %s", filepath.Base(f), name)
			}
		}
	}
}
