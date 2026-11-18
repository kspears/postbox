package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kspears/postbox/internal/mail/mailtest"
)

// poolServer accepts the password in *accept and counts logins and open connections.
func poolServer(t *testing.T, accept *atomic.Value) (*Pool, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	logins, open, peak := &atomic.Int32{}, &atomic.Int32{}, &atomic.Int32{}
	addr := mailtest.Listen(t, "* OK ready\r\n", func(tag, verb, line string) (string, bool) {
		switch verb {
		case "LOGIN":
			logins.Add(1)
			want := accept.Load().(string)
			if strings.HasSuffix(line, " "+want) || strings.HasSuffix(line, `"`+want+`"`) {
				if n := open.Add(1); n > peak.Load() {
					peak.Store(n)
				}
				return tag + " OK logged in\r\n", false
			}
			return tag + " NO [AUTHENTICATIONFAILED] no\r\n", false
		case "LOGOUT":
			open.Add(-1)
			return "* BYE\r\n" + tag + " OK\r\n", true
		}
		return tag + " OK\r\n", false
	})
	p := NewPool(t.Context(), &Server{IMAPAddr: addr, TLS: &tls.Config{InsecureSkipVerify: true}}, testClock.Now)
	return p, logins, peak
}

// testClock is the pool's clock in these tests; only TestPoolLogsInAgainAfterMaxAge moves it.
var testClock = &fakeClock{t: time.Unix(1_800_000_000, 0)}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func acct(password string) Account {
	return Account{ClientIP: ip, Name: "a@example.test", Password: password}
}

func TestPoolReusesConnections(t *testing.T) {
	var accept atomic.Value
	accept.Store("pw")
	p, logins, _ := poolServer(t, &accept)
	for range 3 {
		if err := p.Do(context.Background(), acct("pw"), func(c *Conn) error { return c.Noop().Wait() }); err != nil {
			t.Fatal(err)
		}
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("%d logins for three requests, want 1", n)
	}
}

func TestPoolCapsConnectionsPerAccount(t *testing.T) {
	var accept atomic.Value
	accept.Store("pw")
	p, _, peak := poolServer(t, &accept)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			p.Do(context.Background(), acct("pw"), func(c *Conn) error {
				time.Sleep(30 * time.Millisecond)
				return nil
			})
		})
	}
	wg.Wait()
	if n := peak.Load(); n > commandConns {
		t.Errorf("%d connections open at once, want at most %d", n, commandConns)
	}
}

func TestPoolNoticesAChangedPassword(t *testing.T) {
	var accept atomic.Value
	accept.Store("old")
	p, _, _ := poolServer(t, &accept)
	if err := p.Do(context.Background(), acct("old"), func(*Conn) error { return nil }); err != nil {
		t.Fatal(err)
	}
	accept.Store("new")
	// A session with the new password must not reuse the old login, and one with the old
	// password must be told it no longer works.
	if err := p.Do(context.Background(), acct("new"), func(*Conn) error { return nil }); err != nil {
		t.Errorf("new password: %v", err)
	}
	if err := p.Do(context.Background(), acct("old"), func(*Conn) error { return nil }); !errors.Is(err, ErrRefused) {
		t.Errorf("old password: got %v, want ErrRefused", err)
	}
}

func TestPoolRetiresBrokenConnections(t *testing.T) {
	var accept atomic.Value
	accept.Store("pw")
	p, logins, _ := poolServer(t, &accept)
	err := p.Do(context.Background(), acct("pw"), func(*Conn) error { return errors.New("connection reset") })
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v, want ErrUnreachable", err)
	}
	p.Do(context.Background(), acct("pw"), func(*Conn) error { return nil })
	if n := logins.Load(); n != 2 {
		t.Errorf("%d logins, want 2: the broken connection was reused", n)
	}
}

// A password changed elsewhere is noticed within maxAge even while the session stays busy.
func TestPoolLogsInAgainAfterMaxAge(t *testing.T) {
	var accept atomic.Value
	accept.Store("old")
	p, logins, _ := poolServer(t, &accept)
	use := func() error { return p.Do(context.Background(), acct("old"), func(*Conn) error { return nil }) }
	if err := use(); err != nil {
		t.Fatal(err)
	}
	accept.Store("new")
	testClock.advance(maxAge - time.Second)
	if err := use(); err != nil {
		t.Errorf("before maxAge the connection is reused: %v", err)
	}
	testClock.advance(time.Second)
	if err := use(); !errors.Is(err, ErrRefused) {
		t.Errorf("after maxAge: got %v, want ErrRefused", err)
	}
	if n := logins.Load(); n != 2 {
		t.Errorf("%d logins, want 2", n)
	}
}
