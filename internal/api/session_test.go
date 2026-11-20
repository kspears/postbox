package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/kspears/postbox/internal/addressbook"
	"github.com/kspears/postbox/internal/auth"
	"github.com/kspears/postbox/internal/clock"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/session"
	"github.com/kspears/postbox/internal/store"
)

const origin = "https://mail.example.test"

// newHandler builds the handler with a real store and a mail server that is never reachable,
// so only paths that do not need one can succeed.
func newHandler(t *testing.T, dist fs.FS) http.Handler {
	t.Helper()
	return NewHandler(dist, newDeps(t))
}

func newDeps(t *testing.T) Deps {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clk := &clock.Clock{}
	server := &mail.Server{IMAPAddr: "127.0.0.1:1", TLS: &tls.Config{}}
	pool := mail.NewPool(t.Context(), server, clk.Now)
	authenticator, err := auth.New(st, clk, server, &url.URL{Scheme: "https", Host: "mail.example.test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return Deps{
		Sessions:       session.NewManager(st, clk, true),
		Auth:           authenticator,
		Mail:           pool,
		Addresses:      addressbook.New(st, pool),
		Store:          st,
		Watch:          mail.NewWatcher(server),
		Submit:         &mail.Submitter{Addr: "127.0.0.1:1", TLS: &tls.Config{}},
		Stopping:       t.Context(),
		Clock:          clk,
		Origin:         origin,
		ClientIPHeader: "X-Real-IP",
		TrustedProxy:   netip.MustParseAddr("192.0.2.1"), // httptest's default peer
	}
}

func signIn(t *testing.T, h http.Handler, body, ip string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/session", strings.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("X-Real-IP", ip)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Error string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return body.Error
}

func TestSignInOutcomes(t *testing.T) {
	h := newHandler(t, dist)
	tests := []struct {
		name, body string
		status     int
		code       string
	}{
		{"not JSON", "nope", 400, errBadRequest},
		{"empty password", `{"email":"a@example.test","password":""}`, 401, errWrongCredentials},
		{"mail server down", `{"email":"a@example.test","password":"pw"}`, 502, errUnreachable},
	}
	for _, tt := range tests {
		w := signIn(t, h, tt.body, "203.0.113.1")
		if w.Code != tt.status || errorCode(t, w) != tt.code {
			t.Errorf("%s: got %d %s, want %d %s", tt.name, w.Code, w.Body.String(), tt.status, tt.code)
		}
	}
}

func TestSignInIsThrottled(t *testing.T) {
	h := newHandler(t, dist)
	empty := `{"email":"a@example.test","password":""}`
	for range 5 {
		signIn(t, h, empty, "203.0.113.2")
	}
	if w := signIn(t, h, `{"email":"a@example.test","password":"pw"}`, "203.0.113.2"); w.Code != 429 || errorCode(t, w) != errThrottled {
		t.Errorf("sixth attempt: got %d %s", w.Code, w.Body.String())
	}
	// Another address on the header is another person.
	if w := signIn(t, h, empty, "203.0.113.3"); errorCode(t, w) != errWrongCredentials {
		t.Errorf("another IP: got %d %s", w.Code, w.Body.String())
	}
}

func TestCrossOriginWritesAreRefused(t *testing.T) {
	h := newHandler(t, dist)
	for _, o := range []string{"", "https://evil.example", "https://mail.example.test:8443"} {
		r := httptest.NewRequest("POST", "/api/session", strings.NewReader(`{}`))
		if o != "" {
			r.Header.Set("Origin", o)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("Origin %q: got %d", o, w.Code)
		}
	}
}

func TestSignedOutRequests(t *testing.T) {
	h := newHandler(t, dist)
	if w := get(t, h, "GET", "/api/session"); w.Code != 401 || errorCode(t, w) != errSignedOut {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/session", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-postbox_session", Value: "forged.AAAA"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("a dead cookie was not removed: %d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := get(t, newHandler(t, dist), "GET", "/")
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy"} {
		if w.Header().Get(h) == "" {
			t.Errorf("no %s", h)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("the app can be framed by other sites")
	}
}

func TestClientIP(t *testing.T) {
	a := &apiHandler{Deps: Deps{ClientIPHeader: "X-Forwarded-For", TrustedProxy: netip.MustParseAddr("192.0.2.1")}}
	tests := []struct {
		header []string
		remote string
		want   string
	}{
		{nil, "192.0.2.1:5000", "192.0.2.1"},
		{[]string{"203.0.113.9"}, "192.0.2.1:5000", "203.0.113.9"},
		{[]string{"1.1.1.1, 203.0.113.9"}, "192.0.2.1:5000", "203.0.113.9"},
		{[]string{"1.1.1.1", "203.0.113.9"}, "192.0.2.1:5000", "203.0.113.9"},
		{[]string{"garbage"}, "192.0.2.1:5000", "192.0.2.1"},
		{[]string{"::ffff:203.0.113.9"}, "192.0.2.1:5000", "203.0.113.9"},
		{nil, "[2001:db8::1]:5000", "2001:db8::1"},
		// Only the trusted proxy's header is believed.
		{[]string{"203.0.113.9"}, "192.0.2.99:5000", "192.0.2.99"},
		// A zone can hold any text, quotes included; it must never reach the mail server.
		{[]string{`fe80::1%x" "x-forward-user" "admin`}, "192.0.2.1:5000", "fe80::1"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remote
		for _, v := range tt.header {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := a.clientIP(r); got != netip.MustParseAddr(tt.want) {
			t.Errorf("%v from %s: got %s, want %s", tt.header, tt.remote, got, tt.want)
		}
	}
	unset := &apiHandler{}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.1:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := unset.clientIP(r); got.String() != "192.0.2.1" {
		t.Errorf("header trusted without POSTBOX_CLIENT_IP_HEADER: %s", got)
	}
}

// The test clock exists only in binaries built with -tags citest; `go test` builds without it.
func TestReleaseBuildHasNoTestClock(t *testing.T) {
	h := newHandler(t, dist)
	r := httptest.NewRequest("POST", "/api/test/clock", strings.NewReader(`{"seconds":60}`))
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("got %d, want 404", w.Code)
	}
}

// Changing passkeys and "Require passkey" needs the confirmation token the signing-in tab was
// given: the session cookie alone, as a thief would have it, is refused.
func TestPasskeyChangesNeedTheTabsToken(t *testing.T) {
	deps := newDeps(t)
	h := NewHandler(dist, deps)
	cookie, token, err := deps.Sessions.Create(t.Context(), "a@example.test", "pw", session.Password, false)
	if err != nil {
		t.Fatal(err)
	}
	send := func(method, path, body, confirm string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.AddCookie(cookie)
		if confirm != "" {
			r.Header.Set(confirmHeader, confirm)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	gated := []struct{ method, path, body string }{
		{"POST", "/api/passkeys/options", ``},
		{"POST", "/api/passkeys/delete", `{"id":"AAAA"}`},
		{"PUT", "/api/require-passkey", `{"on":false}`},
	}
	for _, g := range gated {
		if w := send(g.method, g.path, g.body, ""); w.Code != 403 || errorCode(t, w) != errConfirmNeeded {
			t.Errorf("%s %s without the token: %d %s", g.method, g.path, w.Code, w.Body.String())
		}
		if w := send(g.method, g.path, g.body, "guess"); w.Code != 403 {
			t.Errorf("%s %s with a wrong token: %d", g.method, g.path, w.Code)
		}
		if w := send(g.method, g.path, g.body, token); w.Code == 403 {
			t.Errorf("%s %s with the token: %d %s", g.method, g.path, w.Code, w.Body.String())
		}
	}

	// Entering the password again gives a new token; a wrong password gives none.
	if w := send("POST", "/api/session/confirm", `{"password":"nope"}`, ""); w.Code != 401 || errorCode(t, w) != errWrongCredentials {
		t.Errorf("wrong password: %d %s", w.Code, w.Body.String())
	}
	w := send("POST", "/api/session/confirm", `{"password":"pw"}`, "")
	var got struct{ Confirm string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Confirm == "" || got.Confirm == token {
		t.Fatalf("right password: %d %s", w.Code, w.Body.String())
	}
	if w := send("PUT", "/api/require-passkey", `{"on":false}`, got.Confirm); w.Code != 204 {
		t.Errorf("with the new token: %d %s", w.Code, w.Body.String())
	}
}
