// Package auth checks a person's email and password against the mail server, with throttling
// in front so that guessing is slow and blocked attempts never reach the server.
package auth

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

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
)

// Authenticator signs people in.
type Authenticator struct {
	store  *store.Store
	clock  *clock.Clock
	server *mail.Server
}

// New returns an Authenticator.
func New(st *store.Store, clk *clock.Clock, server *mail.Server) *Authenticator {
	return &Authenticator{store: st, clock: clk, server: server}
}

// Account turns what a person typed into the account name used everywhere else.
func Account(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CheckPassword confirms account and password with the mail server. The error is
// ErrThrottled, ErrWrongPassword, or wraps mail.ErrUnreachable.
func (a *Authenticator) CheckPassword(ctx context.Context, clientIP netip.Addr, account, password string) error {
	ip := clientIP.Unmap().String()
	pairKey, ipKey := account+" "+ip, ip

	blocked, err := a.store.Blocked(ctx, a.clock.Now(), pairKey, ipKey)
	if err != nil {
		return err
	}
	if blocked {
		return ErrThrottled
	}
	if account == "" || password == "" {
		return a.fail(ctx, account, ip, pairKey, ipKey)
	}

	c, err := a.server.Login(ctx, clientIP, account, password)
	if errors.Is(err, mail.ErrRefused) {
		return a.fail(ctx, account, ip, pairKey, ipKey)
	}
	if err != nil {
		return err
	}
	c.Logout().Wait()
	c.Close()
	return a.store.ClearFailures(ctx, account, ip)
}

func (a *Authenticator) fail(ctx context.Context, account, ip, pairKey, ipKey string) error {
	err := a.store.RecordFailure(ctx, a.clock.Now(), account, ip,
		store.FailureLimit{Key: pairKey, Account: account, Max: maxPerAccountIP, Window: window, Block: block},
		store.FailureLimit{Key: ipKey, Max: maxPerIP, Window: window, Block: block},
	)
	if err != nil {
		return err
	}
	return ErrWrongPassword
}
