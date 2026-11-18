package mail

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/netip"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Each account gets at most this many command connections, opened on demand. Dovecot allows 10
// per user and IP; with one more for change notifications, this leaves room for the person's
// other mail apps on the same network.
const commandConns = 2

// A connection unused for this long is logged out.
const idleTimeout = 5 * time.Minute

// A connection is not reused once it is this old. Dovecot keeps a logged-in connection working
// after the password changes, so without a fresh login now and then a session would never learn
// that its password no longer works.
const maxAge = 10 * time.Minute

// Pool keeps logged-in IMAP connections per account so requests do not pay for a login each.
type Pool struct {
	server *Server
	now    func() time.Time

	mu       sync.Mutex
	accounts map[string]*accountConns
}

type accountConns struct {
	slots    chan struct{} // one token per connection that may exist
	password [32]byte      // SHA-256 of the password the idle connections logged in with
	idle     []idleConn
}

type idleConn struct {
	c       *imapclient.Client
	since   time.Time // returned to the pool
	created time.Time // logged in
}

// NewPool returns a Pool and starts closing connections that stay unused. now is the clock
// connection ages are measured with.
func NewPool(ctx context.Context, server *Server, now func() time.Time) *Pool {
	p := &Pool{server: server, now: now, accounts: map[string]*accountConns{}}
	go p.closeIdle(ctx)
	return p
}

// Conn is a connection lent out by the pool. Call Release when done.
type Conn struct {
	*imapclient.Client
	pool     *Pool
	account  string
	password [32]byte
	created  time.Time
	broken   bool
}

// Broken marks the connection as unusable, so Release closes it instead of keeping it.
func (c *Conn) Broken() { c.broken = true }

// Release returns the connection to the pool.
func (c *Conn) Release() {
	c.pool.put(c)
}

// Get lends out a logged-in connection for account. It waits while the account's connections
// are all in use. The error is ErrRefused when the server no longer accepts password, or wraps
// ErrUnreachable.
func (p *Pool) Get(ctx context.Context, clientIP netip.Addr, account, password string) (*Conn, error) {
	sum := sha256.Sum256([]byte(password))
	a := p.account(account)
	select {
	case a.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if ic, ok := p.takeIdle(a, sum); ok {
		return &Conn{Client: ic.c, pool: p, account: account, password: sum, created: ic.created}, nil
	}
	c, err := p.server.Login(ctx, clientIP, account, password)
	if err != nil {
		<-a.slots
		return nil, err
	}
	return &Conn{Client: c, pool: p, account: account, password: sum, created: p.now()}, nil
}

func (p *Pool) account(name string) *accountConns {
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.accounts[name]
	if a == nil {
		a = &accountConns{slots: make(chan struct{}, commandConns)}
		p.accounts[name] = a
	}
	return a
}

// takeIdle returns an idle connection that logged in with the same password, or nil. A request
// with a different password means the password changed: the old connections are closed, so the
// new password (or the old one, from an old session) is checked by a fresh login.
func (p *Pool) takeIdle(a *accountConns, password [32]byte) (idleConn, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if a.password != password {
		for _, ic := range a.idle {
			go ic.c.Close()
		}
		a.idle, a.password = nil, password
		return idleConn{}, false
	}
	for len(a.idle) > 0 {
		ic := a.idle[len(a.idle)-1]
		a.idle = a.idle[:len(a.idle)-1]
		select {
		case <-ic.c.Closed():
			continue
		default:
		}
		if p.now().Sub(ic.created) >= maxAge {
			go p.server.Logout(ic.c)
			continue
		}
		return ic, true
	}
	return idleConn{}, false
}

func (p *Pool) put(c *Conn) {
	p.mu.Lock()
	a := p.accounts[c.account]
	keep := !c.broken && a.password == c.password
	if keep {
		select {
		case <-c.Client.Closed():
			keep = false
		default:
		}
	}
	if keep {
		a.idle = append(a.idle, idleConn{c: c.Client, since: p.now(), created: c.created})
	}
	p.mu.Unlock()
	if !keep {
		c.Client.Close()
	}
	<-a.slots
}

func (p *Pool) closeIdle(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.closeAll()
			return
		case <-ticker.C:
		}
		var stale []*imapclient.Client
		p.mu.Lock()
		for _, a := range p.accounts {
			kept := a.idle[:0]
			for _, ic := range a.idle {
				if p.now().Sub(ic.since) > idleTimeout {
					stale = append(stale, ic.c)
				} else {
					kept = append(kept, ic)
				}
			}
			a.idle = kept
		}
		p.mu.Unlock()
		for _, c := range stale {
			p.server.Logout(c)
		}
	}
}

func (p *Pool) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, a := range p.accounts {
		for _, ic := range a.idle {
			ic.c.Close()
		}
		a.idle = nil
	}
}

// Account is what Do needs to act for a signed-in person.
type Account struct {
	ClientIP netip.Addr
	Name     string
	Password string
}

// Do runs fn on a pooled connection for acct. A connection whose command failed for any reason
// other than the server's own NO or BAD answer is closed rather than reused. Login errors are
// returned as Get returns them.
func (p *Pool) Do(ctx context.Context, acct Account, fn func(*Conn) error) error {
	c, err := p.Get(ctx, acct.ClientIP, acct.Name, acct.Password)
	if err != nil {
		return err
	}
	defer c.Release()
	err = fn(c)
	var status *imap.Error
	if err != nil && !errors.As(err, &status) {
		c.Broken()
	}
	if err != nil && !errors.As(err, &status) && ctx.Err() == nil {
		return errors.Join(ErrUnreachable, err)
	}
	return err
}
