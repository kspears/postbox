// Package outbox holds sent messages for their undo window and then hands them to the
// submission server, on the server's own clock: closing the tab does not cancel a send.
package outbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"

	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/store"
)

// States a send goes through; see migrations/004_outbox.sql.
const (
	Waiting    = "waiting"
	Submitting = "submitting"
	Sent       = "sent"
	SentNoCopy = "sent_no_copy"
	Failed     = "failed"
	Unsent     = "unsent"
	Unknown    = "unknown"
)

// Window is how long "Undo" is offered after "Send".
var Window = 10 * time.Second

// ErrTooLate means the undo window has passed.
var ErrTooLate = errors.New("the message is already on its way")

// Outbox holds the messages in their undo window.
type Outbox struct {
	store  *store.Store
	pool   *mail.Pool
	submit *mail.Submitter

	mu      sync.Mutex
	waiting map[string]*time.Timer
}

// New returns an Outbox. Messages that were in the window or being handed over when Postbox last
// stopped are settled first (see store.SettleInterruptedSends).
func New(ctx context.Context, st *store.Store, pool *mail.Pool, submit *mail.Submitter) (*Outbox, error) {
	if err := st.SettleInterruptedSends(ctx); err != nil {
		return nil, err
	}
	return &Outbox{store: st, pool: pool, submit: submit, waiting: map[string]*time.Timer{}}, nil
}

// Queue starts the undo window for the draft at draftsFolder/uid and returns the send's id. When
// the window ends, the message is sent with acct's password, which the outbox holds in memory
// only until then.
func (o *Outbox) Queue(ctx context.Context, acct mail.Account, draftsFolder string, uid imap.UID) (string, error) {
	b := make([]byte, 12)
	rand.Read(b)
	id := hex.EncodeToString(b)
	err := o.store.AddSend(ctx, store.Send{
		ID: id, Account: acct.Name, DraftsFolder: draftsFolder, DraftUID: uint32(uid), State: Waiting, CreatedAt: time.Now(),
	})
	if err != nil {
		return "", err
	}
	o.mu.Lock()
	o.waiting[id] = time.AfterFunc(Window, func() { o.send(id, acct, draftsFolder, uid) })
	o.mu.Unlock()
	return id, nil
}

// Undo stops a send that is still in its window and returns the draft it came from.
func (o *Outbox) Undo(ctx context.Context, account, id string) (store.Send, error) {
	send, err := o.store.GetSend(ctx, account, id)
	if err != nil {
		return store.Send{}, err
	}
	o.mu.Lock()
	timer, ok := o.waiting[id]
	stopped := ok && timer.Stop()
	if stopped {
		delete(o.waiting, id)
	}
	o.mu.Unlock()
	if !stopped {
		return store.Send{}, ErrTooLate
	}
	return send, o.store.DeleteSend(ctx, account, id)
}

// Status returns what has become of a send.
func (o *Outbox) Status(ctx context.Context, account, id string) (store.Send, error) {
	return o.store.GetSend(ctx, account, id)
}

func (o *Outbox) send(id string, acct mail.Account, draftsFolder string, uid imap.UID) {
	o.mu.Lock()
	delete(o.waiting, id)
	o.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	state, errText := o.deliver(ctx, id, acct, draftsFolder, uid)
	if err := o.store.SetSendState(context.Background(), id, state, errText); err != nil {
		log.Printf("outbox %s: record %s: %v", id, state, err)
	}
}

// deliver hands the draft to the submission server and files the Sent copy. It returns the
// send's final state and, on failure, the words to show.
func (o *Outbox) deliver(ctx context.Context, id string, acct mail.Account, draftsFolder string, uid imap.UID) (string, string) {
	var raw []byte
	err := o.pool.Do(ctx, acct, func(c *mail.Conn) (err error) {
		raw, err = c.Raw(draftsFolder, uid)
		return err
	})
	if err != nil {
		log.Printf("outbox %s: read draft: %v", id, err)
		return Failed, "The draft could not be read."
	}
	env, forRecipients, err := envelope(raw)
	if err != nil {
		return Failed, err.Error()
	}

	started := false
	err = o.submit.Send(ctx, acct.ClientIP, acct.Name, acct.Password, env, forRecipients, func() {
		started = true
		// Recorded before the first byte goes, so a stop from here on is known to be ambiguous.
		if err := o.store.SetSendState(ctx, id, Submitting, ""); err != nil {
			log.Printf("outbox %s: record submitting: %v", id, err)
		}
	})
	var rejected *mail.Rejected
	switch {
	case errors.As(err, &rejected):
		return Failed, rejected.Text
	case errors.Is(err, mail.ErrRefused):
		return Failed, "The mail server refused the password."
	case err != nil && started:
		log.Printf("outbox %s: %v", id, err)
		return Unknown, ""
	case err != nil:
		log.Printf("outbox %s: %v", id, err)
		return Failed, "The mail server could not be reached."
	}

	// Sent. The copy goes to Sent with its Bcc line, as the sender should see it.
	err = o.pool.Do(ctx, acct, func(c *mail.Conn) error {
		if err := c.FileSent(raw); err != nil {
			return err
		}
		return c.DiscardDraft(draftsFolder, uid)
	})
	if err != nil {
		log.Printf("outbox %s: file in Sent: %v", id, err)
		return SentNoCopy, ""
	}
	return Sent, ""
}

// envelope reads who a saved message is from and to, and returns the copy the recipients get:
// the same message without its Bcc line.
func envelope(raw []byte) (mail.Envelope, []byte, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return mail.Envelope{}, nil, err
	}
	h := gomail.Header{Header: entity.Header}
	var env mail.Envelope
	if from, err := h.AddressList("From"); err == nil && len(from) == 1 {
		env.From = from[0].Address
	}
	for _, key := range []string{"To", "Cc", "Bcc"} {
		addrs, _ := h.AddressList(key)
		for _, a := range addrs {
			env.To = append(env.To, a.Address)
		}
	}
	if env.From == "" {
		return env, nil, errors.New("The message has no sender.")
	}
	if len(env.To) == 0 {
		return env, nil, errors.New("The message has no recipients.")
	}
	return env, withoutBcc(raw), nil
}

// withoutBcc removes the Bcc header (and its folded lines) from a message's top header.
func withoutBcc(raw []byte) []byte {
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		return raw
	}
	lines := strings.SplitAfter(string(raw[:end+2]), "\r\n")
	var out strings.Builder
	dropping := false
	for _, line := range lines {
		folded := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if !folded {
			dropping = strings.HasPrefix(strings.ToLower(line), "bcc:")
		}
		if !dropping {
			out.WriteString(line)
		}
	}
	return append([]byte(out.String()), raw[end+2:]...)
}
