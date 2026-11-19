// Package addressbook remembers the addresses a person has sent mail to and offers them for
// autocomplete in the recipient fields.
package addressbook

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/store"
)

// matchLimit is how many addresses one lookup offers.
const matchLimit = 8

// seededKey is the account setting that says the Sent folder has been read.
const seededKey = "addresses_seeded"

// seedTimeout bounds one reading of a Sent folder.
const seedTimeout = 10 * time.Minute

// Book collects and looks up addresses.
type Book struct {
	store *store.Store
	pool  *mail.Pool

	mu      sync.Mutex
	seeding map[string]bool
}

// New returns a Book that keeps its addresses in st and reads Sent folders through pool.
func New(st *store.Store, pool *mail.Pool) *Book {
	return &Book{store: st, pool: pool, seeding: map[string]bool{}}
}

// Collect remembers people as addresses account has sent mail to just now.
func (b *Book) Collect(ctx context.Context, account string, people []store.Address) {
	if err := b.store.CollectAddresses(ctx, account, people, time.Now()); err != nil {
		log.Printf("addressbook %s: collect: %v", account, err)
	}
}

// Match returns the addresses whose name or address starts with prefix.
func (b *Book) Match(ctx context.Context, account, prefix string) ([]store.Address, error) {
	return b.store.MatchAddresses(ctx, account, prefix, matchLimit)
}

// SeedOnce collects the recipients already in acct's Sent folder, in the background, unless that
// has been done before or is under way. It returns at once.
func (b *Book) SeedOnce(acct mail.Account) {
	b.mu.Lock()
	if b.seeding[acct.Name] {
		b.mu.Unlock()
		return
	}
	b.seeding[acct.Name] = true
	b.mu.Unlock()
	go func() {
		defer func() {
			b.mu.Lock()
			delete(b.seeding, acct.Name)
			b.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
		defer cancel()
		if err := b.seed(ctx, acct); err != nil {
			log.Printf("addressbook %s: read the Sent folder: %v", acct.Name, err)
		}
	}()
}

func (b *Book) seed(ctx context.Context, acct mail.Account) error {
	var done bool
	if err := b.store.Setting(ctx, acct.Name, seededKey, &done); err != nil || done {
		return err
	}
	var found []mail.Recipient
	err := b.pool.Do(ctx, acct, func(c *mail.Conn) (err error) {
		found, err = c.SentRecipients()
		return err
	})
	if err != nil {
		return err
	}
	now := time.Now()
	for _, r := range found {
		at := r.Date
		if at.IsZero() {
			at = now
		}
		if err := b.store.CollectAddresses(ctx, acct.Name, []store.Address{{Name: r.Name, Email: r.Email}}, at); err != nil {
			return err
		}
	}
	return b.store.SetSetting(ctx, acct.Name, seededKey, true)
}
