package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kspears/postbox/internal/clock"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/mail/mailtest"
	"github.com/kspears/postbox/internal/store"
)

const rightPassword = "right"

// fakeServer is an IMAP server that accepts rightPassword for every account, answers wrong
// ones after a short pause as a real server does, and counts LOGIN commands.
func fakeServer(t *testing.T) (*mail.Server, *atomic.Int32) {
	t.Helper()
	logins := &atomic.Int32{}
	addr := mailtest.Listen(t, "* OK ready\r\n", func(tag, verb, line string) (string, bool) {
		switch verb {
		case "LOGIN":
			logins.Add(1)
			time.Sleep(20 * time.Millisecond)
			if strings.HasSuffix(line, " "+rightPassword) || strings.HasSuffix(line, `"`+rightPassword+`"`) {
				return tag + " OK logged in\r\n", false
			}
			return tag + " NO [AUTHENTICATIONFAILED] Authentication failed.\r\n", false
		case "LOGOUT":
			return "* BYE\r\n" + tag + " OK bye\r\n", true
		}
		return tag + " OK\r\n", false
	})
	return &mail.Server{IMAPAddr: addr, TLS: &tls.Config{InsecureSkipVerify: true}}, logins
}

func newAuth(t *testing.T, server *mail.Server) (*Authenticator, *clock.Clock) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clk := &clock.Clock{}
	return New(st, clk, server), clk
}

func newFakeAuth(t *testing.T) (*Authenticator, *atomic.Int32, *clock.Clock) {
	t.Helper()
	server, logins := fakeServer(t)
	a, clk := newAuth(t, server)
	return a, logins, clk
}

var client = netip.MustParseAddr("203.0.113.5")

func TestRightPassword(t *testing.T) {
	a, _, _ := newFakeAuth(t)
	if err := a.CheckPassword(context.Background(), client, "a@example.test", rightPassword); err != nil {
		t.Fatal(err)
	}
}

func TestBlockedAttemptsNeverReachTheServer(t *testing.T) {
	a, logins, clk := newFakeAuth(t)
	ctx := context.Background()
	for range 5 {
		if err := a.CheckPassword(ctx, client, "a@example.test", "wrong"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("got %v", err)
		}
	}
	if err := a.CheckPassword(ctx, client, "a@example.test", rightPassword); !errors.Is(err, ErrThrottled) {
		t.Fatalf("sixth attempt: got %v", err)
	}
	if n := logins.Load(); n != 5 {
		t.Errorf("server saw %d logins, want 5", n)
	}
	clk.Advance(block)
	if err := a.CheckPassword(ctx, client, "a@example.test", rightPassword); err != nil {
		t.Errorf("after the block: %v", err)
	}
}

// Parallel requests must not all pass the block check before the first failure is recorded.
func TestParallelGuessesAreStillLimited(t *testing.T) {
	a, logins, _ := newFakeAuth(t)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			a.CheckPassword(context.Background(), client, "a@example.test", "wrong")
		})
	}
	wg.Wait()
	if n := logins.Load(); n != 5 {
		t.Errorf("server saw %d logins from 50 parallel guesses, want 5", n)
	}
}

func TestEmptyPasswordCountsWithoutAskingTheServer(t *testing.T) {
	a, logins, _ := newFakeAuth(t)
	for range 5 {
		if err := a.CheckPassword(context.Background(), client, "a@example.test", ""); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("got %v", err)
		}
	}
	if err := a.CheckPassword(context.Background(), client, "a@example.test", rightPassword); !errors.Is(err, ErrThrottled) {
		t.Errorf("got %v", err)
	}
	if n := logins.Load(); n != 0 {
		t.Errorf("server saw %d logins", n)
	}
}

func TestIPv6CountsByPrefix(t *testing.T) {
	a, logins, _ := newFakeAuth(t)
	for i := range 5 {
		ip := netip.MustParseAddr(fmt.Sprintf("2001:db8:1:2::%x", i+1))
		a.CheckPassword(context.Background(), ip, "a@example.test", "wrong")
	}
	other := netip.MustParseAddr("2001:db8:1:2:ffff::9")
	if err := a.CheckPassword(context.Background(), other, "a@example.test", rightPassword); !errors.Is(err, ErrThrottled) {
		t.Errorf("another address in the same /64: got %v", err)
	}
	elsewhere := netip.MustParseAddr("2001:db8:1:3::1")
	if err := a.CheckPassword(context.Background(), elsewhere, "a@example.test", rightPassword); err != nil {
		t.Errorf("another /64: got %v", err)
	}
	if n := logins.Load(); n != 6 {
		t.Errorf("server saw %d logins, want 6", n)
	}
}

func TestThrottleKey(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.5":          "203.0.113.5",
		"::ffff:203.0.113.5":   "203.0.113.5",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"fe80::1%eth0":         "fe80::/64",
	} {
		if got := throttleKey(netip.MustParseAddr(in)); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestUnreachableIsNotAFailure(t *testing.T) {
	a, _ := newAuth(t, &mail.Server{IMAPAddr: "127.0.0.1:1", TLS: &tls.Config{}})
	for range 6 {
		if err := a.CheckPassword(context.Background(), client, "a@example.test", "pw"); !errors.Is(err, mail.ErrUnreachable) {
			t.Fatalf("got %v", err)
		}
	}
}
