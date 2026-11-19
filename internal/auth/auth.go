// Package auth checks a person's email and password against the mail server, with throttling
// in front so that guessing is slow and blocked attempts never reach the server.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/kspears/postbox/internal/clock"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/store"
)

// Outcomes of a sign-in other than success.
var (
	ErrWrongPassword = errors.New("wrong email or password")
	ErrThrottled     = errors.New("too many attempts")
)

// The spec's limits: 5 failures for one address from one IP, or 20 from one IP across every
// address, within 15 minutes block further attempts for 15 minutes.
const (
	window          = 15 * time.Minute
	block           = 15 * time.Minute
	maxPerAccountIP = 5
	maxPerIP        = 20

	// The longest a sign-in may wait for the mail server. It also bounds how long one slow
	// attempt holds up the next from the same IP.
	loginTimeout = 30 * time.Second
)

// Authenticator signs people in.
type Authenticator struct {
	store  *store.Store
	clock  *clock.Clock
	server *mail.Server
	// One attempt at a time per client IP: checking the block, asking the mail server and
	// recording the result happen as one step, so parallel requests cannot all slip past the
	// check before the first failure is written.
	inFlight keyedMutex

	webauthn *webauthn.WebAuthn
	pending  ceremonies
}

// New returns an Authenticator for Postbox served at public.
func New(st *store.Store, clk *clock.Clock, server *mail.Server, public *url.URL) (*Authenticator, error) {
	wa, err := newWebAuthn(public)
	if err != nil {
		return nil, fmt.Errorf("POSTBOX_PUBLIC_URL: %w", err)
	}
	return &Authenticator{store: st, clock: clk, server: server, webauthn: wa}, nil
}

// Account turns what a person typed into the account name used everywhere else.
func Account(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// throttleKey is the address the limits count against. An IPv6 address counts as its /64,
// because one ordinary connection is given a whole /64 and could otherwise use a fresh address
// for every attempt.
func throttleKey(ip netip.Addr) string {
	ip = ip.Unmap().WithZone("")
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
}

// CheckPassword confirms account and password with the mail server. The error is
// ErrThrottled, ErrWrongPassword, or wraps mail.ErrUnreachable.
func (a *Authenticator) CheckPassword(ctx context.Context, clientIP netip.Addr, account, password string) error {
	ip := throttleKey(clientIP)
	unlock := a.inFlight.lock(ip)
	defer unlock()

	if err := a.checkBlocked(ctx, account, ip); err != nil {
		return err
	}
	if account == "" || password == "" {
		return a.fail(ctx, account, ip)
	}

	loginCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	c, err := a.server.Login(loginCtx, clientIP, account, password)
	if errors.Is(err, mail.ErrRefused) {
		return a.fail(ctx, account, ip)
	}
	if err != nil {
		return err
	}
	a.server.Logout(c)
	// The password is right; failing to tidy the counters must not turn that into an error.
	if err := a.store.ClearFailures(context.WithoutCancel(ctx), account, ip); err != nil {
		log.Printf("sign-in: clear failures: %v", err)
	}
	return nil
}

// checkBlocked returns ErrThrottled when account may not sign in from ip now.
func (a *Authenticator) checkBlocked(ctx context.Context, account, ip string) error {
	blocked, err := a.store.Blocked(ctx, a.clock.Now(), account, ip)
	if err != nil {
		return err
	}
	if blocked {
		return ErrThrottled
	}
	return nil
}

func (a *Authenticator) fail(ctx context.Context, account, ip string) error {
	// The mail server has answered; the failure counts even if the browser has gone away.
	err := a.store.RecordFailure(context.WithoutCancel(ctx), a.clock.Now(), account, ip, store.Limits{
		PerAccountIP: maxPerAccountIP, PerIP: maxPerIP, Window: window, Block: block,
	})
	if err != nil {
		return err
	}
	return ErrWrongPassword
}

// keyedMutex is a set of mutexes created on demand and dropped when no one holds or waits for
// them.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	sync.Mutex
	users int
}

func (k *keyedMutex) lock(key string) (unlock func()) {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	l := k.locks[key]
	if l == nil {
		l = &keyedLock{}
		k.locks[key] = l
	}
	l.users++
	k.mu.Unlock()

	l.Lock()
	return func() {
		l.Unlock()
		k.mu.Lock()
		if l.users--; l.users == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}
