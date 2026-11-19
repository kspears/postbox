// Package session turns a successful sign-in into a browser cookie and back.
//
// The cookie carries a random session id and a random 32-byte key. The database row for the id
// holds the account password sealed with that key (AES-256-GCM). The key is never written
// anywhere on the server, so neither the database nor the cookie alone reveals the password,
// and a forged or stale cookie fails to unseal.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kspears/postbox/internal/clock"
	"github.com/kspears/postbox/internal/secret"
	"github.com/kspears/postbox/internal/store"
)

// How sessions are started; stored with each one.
const (
	Password = store.MethodPassword
	Passkey  = store.MethodPasskey
)

// Lifetimes from the spec: 30 days for passkey and "Keep me signed in" sessions; a browser
// session capped at 24 hours otherwise.
const (
	LongLife  = 30 * 24 * time.Hour
	ShortLife = 24 * time.Hour
)

// ErrNoSession means the request carries no usable session: none, expired, signed out, or
// forged. The browser should show the sign-in screen.
var ErrNoSession = errors.New("no session")

// Session is a signed-in account with its password unsealed.
type Session struct {
	ID             string
	Account        string
	Password       string
	Method         string
	confirmedUntil time.Time
	confirmHash    []byte
}

// ConfirmLife is how long signing in, or entering the password again, lets a browser tab change
// passkeys and "Require passkey". The tab proves it with a confirmation token that only it
// holds, so a copy of the session cookie alone cannot.
const ConfirmLife = 10 * time.Minute

// newConfirmation returns a confirmation token and its SHA-256, which is all the server keeps.
func newConfirmation() (string, []byte) {
	token := rand.Text()
	sum := sha256.Sum256([]byte(token))
	return token, sum[:]
}

// Manager creates and checks sessions.
type Manager struct {
	store  *store.Store
	clock  *clock.Clock
	cookie string
	secure bool
}

// NewManager returns a Manager. secure is true when Postbox is served over https; the cookie is
// then marked Secure and takes the __Host- prefix, which pins it to this exact origin.
func NewManager(st *store.Store, clk *clock.Clock, secure bool) *Manager {
	name := "postbox_session"
	if secure {
		name = "__Host-postbox_session"
	}
	return &Manager{store: st, clock: clk, cookie: name, secure: secure}
}

// Create starts a session for account and returns the cookie that carries it, and the
// confirmation token that lets this tab change passkeys for ConfirmLife. keep is the "Keep me
// signed in" box; passkey sessions always last long.
func (m *Manager) Create(ctx context.Context, account, password, method string, keep bool) (*http.Cookie, string, error) {
	idText, key := rand.Text(), make([]byte, 32)
	rand.Read(key)
	confirmToken, confirmHash := newConfirmation()

	sealed, err := seal(key, idText, account, password)
	if err != nil {
		return nil, "", err
	}
	now := m.clock.Now()
	long := keep || method == Passkey
	life := ShortLife
	if long {
		life = LongLife
	}
	err = m.store.CreateSession(ctx, store.Session{
		ID: idText, Account: account, Sealed: sealed, Method: method,
		ConfirmedUntil: now.Add(ConfirmLife), ConfirmHash: confirmHash,
		CreatedAt: now, ExpiresAt: now.Add(life),
	})
	if err != nil {
		return nil, "", err
	}

	c := m.newCookie(idText + "." + base64.RawURLEncoding.EncodeToString(key))
	if long {
		// Max-Age counts from the browser's own clock; the server's expiry is what decides.
		c.MaxAge = int(life / time.Second)
	}
	return c, confirmToken, nil
}

// FromRequest returns the request's session, or ErrNoSession.
func (m *Manager) FromRequest(r *http.Request) (*Session, error) {
	c, err := r.Cookie(m.cookie)
	if err != nil {
		return nil, ErrNoSession
	}
	idText, keyText, ok := strings.Cut(c.Value, ".")
	key, err := base64.RawURLEncoding.DecodeString(keyText)
	if !ok || err != nil || len(key) != 32 {
		return nil, ErrNoSession
	}
	row, err := m.store.Session(r.Context(), idText)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if !m.clock.Now().Before(row.ExpiresAt) {
		m.store.DeleteSession(r.Context(), idText)
		return nil, ErrNoSession
	}
	password, err := open(key, idText, row.Account, row.Sealed)
	if err != nil {
		// The id is right but the key is not: a forged or corrupted cookie.
		return nil, ErrNoSession
	}
	return &Session{
		ID: idText, Account: row.Account, Password: password, Method: row.Method,
		confirmedUntil: row.ConfirmedUntil, confirmHash: row.ConfirmHash,
	}, nil
}

// Confirmed reports whether a request of s carrying token may change passkeys and "Require
// passkey" now.
func (m *Manager) Confirmed(s *Session, token string) bool {
	if token == "" || s.confirmHash == nil || !m.clock.Now().Before(s.confirmedUntil) {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(sum[:], s.confirmHash) == 1
}

// Confirm lets the tab that entered the password change passkeys and "Require passkey" for
// ConfirmLife from now, and returns the token it proves that with. The caller has checked the
// password. A session ended meanwhile is ErrNoSession.
func (m *Manager) Confirm(ctx context.Context, s *Session) (string, error) {
	token, hash := newConfirmation()
	until := m.clock.Now().Add(ConfirmLife)
	err := m.store.ConfirmSession(ctx, s.ID, hash, until)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrNoSession
	}
	if err != nil {
		return "", err
	}
	s.confirmedUntil, s.confirmHash = until, hash
	return token, nil
}

// End signs out one session and returns a cookie that removes it from the browser.
func (m *Manager) End(ctx context.Context, s *Session) (*http.Cookie, error) {
	if err := m.store.DeleteSession(ctx, s.ID); err != nil {
		return nil, err
	}
	return m.Clear(), nil
}

// EndAll signs out every session of the account, this one included.
func (m *Manager) EndAll(ctx context.Context, s *Session) (*http.Cookie, error) {
	if err := m.store.DeleteAccountSessions(ctx, s.Account); err != nil {
		return nil, err
	}
	return m.Clear(), nil
}

// Clear returns a cookie that removes the session cookie from the browser.
func (m *Manager) Clear() *http.Cookie {
	c := m.newCookie("")
	c.MaxAge = -1
	return c
}

func (m *Manager) newCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     m.cookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// The session id and account are bound into the ciphertext, so a sealed password cannot be
// moved to another row.
func additionalData(id, account string) []byte {
	return []byte("postbox session v1\x00" + id + "\x00" + account)
}

func seal(key []byte, id, account, password string) ([]byte, error) {
	return secret.Seal(key, additionalData(id, account), password)
}

func open(key []byte, id, account string, sealed []byte) (string, error) {
	return secret.Open(key, additionalData(id, account), sealed)
}
