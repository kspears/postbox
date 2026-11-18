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

	"github.com/kspears/postbox/internal/clock"
	"github.com/kspears/postbox/internal/mail/mailtest"
)

// fakePool is a Pool in front of a scripted server that accepts the password in accept and
// counts logins and the most connections open at once. Its clock moves only when a test says.
type fakePool struct {
	*Pool
	accept       *atomic.Value
	logins, peak *atomic.Int32
	clock        *clock.Clock
}

func testServer(addr string) *Server {
	return &Server{IMAPAddr: addr, TLS: &tls.Config{InsecureSkipVerify: true}}
}

func poolServer(t *testing.T, password string) fakePool {
	t.Helper()
	accept := &atomic.Value{}
	accept.Store(password)
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
	clk := &clock.Clock{}
	return fakePool{NewPool(t.Context(), testServer(addr), clk.Now), accept, logins, peak, clk}
}

func acct(password string) Account {
	return Account{ClientIP: ip, Name: "a@example.test", Password: password}
}

func TestPoolReusesConnections(t *testing.T) {
	p := poolServer(t, "pw")
	logins := p.logins
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
	p := poolServer(t, "pw")
	peak := p.peak
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
	p := poolServer(t, "old")
	if err := p.Do(context.Background(), acct("old"), func(*Conn) error { return nil }); err != nil {
		t.Fatal(err)
	}
	p.accept.Store("new")
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
	p := poolServer(t, "pw")
	logins := p.logins
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
	p := poolServer(t, "old")
	logins := p.logins
	use := func() error { return p.Do(context.Background(), acct("old"), func(*Conn) error { return nil }) }
	if err := use(); err != nil {
		t.Fatal(err)
	}
	p.accept.Store("new")
	p.clock.Advance(maxAge - time.Second)
	if err := use(); err != nil {
		t.Errorf("before maxAge the connection is reused: %v", err)
	}
	p.clock.Advance(time.Second)
	if err := use(); !errors.Is(err, ErrRefused) {
		t.Errorf("after maxAge: got %v, want ErrRefused", err)
	}
	if n := logins.Load(); n != 2 {
		t.Errorf("%d logins, want 2", n)
	}
}

// A server that stops answering mid-command must not hold the account's connections for ever.
func TestPoolGivesUpOnAHungServer(t *testing.T) {
	old := commandTimeout
	commandTimeout = 200 * time.Millisecond
	t.Cleanup(func() { commandTimeout = old })

	addr := mailtest.Listen(t, "* OK ready\r\n", func(tag, verb, line string) (string, bool) {
		if verb == "NOOP" {
			return "", false // never answer
		}
		return tag + " OK\r\n", false
	})
	p := NewPool(t.Context(), testServer(addr), (&clock.Clock{}).Now)
	hang := func(c *Conn) error { return c.Noop().Wait() }

	var wg sync.WaitGroup
	for range commandConns + 2 {
		wg.Go(func() {
			start := time.Now()
			err := p.Do(context.Background(), acct("pw"), hang)
			if !errors.Is(err, ErrUnreachable) {
				t.Errorf("got %v, want ErrUnreachable", err)
			}
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("took %s", took)
			}
		})
	}
	wg.Wait()
	// The hung connections were closed, so the next call logs in afresh and works.
	if err := p.Do(context.Background(), acct("pw"), func(*Conn) error { return nil }); err != nil {
		t.Errorf("after the hang: %v", err)
	}
}

func TestPoolStopsWaitingWhenTheRequestEnds(t *testing.T) {
	p := poolServer(t, "pw")
	block := make(chan struct{})
	defer close(block)
	for range commandConns {
		go p.Do(context.Background(), acct("pw"), func(*Conn) error { <-block; return nil })
	}
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.Do(ctx, acct("pw"), func(*Conn) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want the request's own deadline", err)
	}
}
