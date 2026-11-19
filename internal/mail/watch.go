package mail

import (
	"context"
	"crypto/sha256"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

// Event is news for the browser about an account.
type Event string

const (
	EventInbox           Event = "inbox"            // something in the Inbox changed
	EventUnreachable     Event = "unreachable"      // the mail server stopped answering
	EventReachable       Event = "reachable"        // it answers again
	EventPasswordChanged Event = "password_changed" // it refuses the session's password
)

// How long to wait before trying the mail server again, and how long one IDLE may last: Dovecot
// ends it after 30 minutes without traffic.
var (
	retryAfter  = 5 * time.Second
	idleRestart = 25 * time.Minute
)

// Watcher keeps one connection per account waiting in IDLE on the Inbox and tells every tab of
// that account what changes, however many tabs there are.
type Watcher struct {
	server *Server

	mu   sync.Mutex
	hubs map[hubKey]*hub
}

type hubKey struct {
	account  string
	password [32]byte
}

type hub struct {
	cancel context.CancelFunc
	subs   map[chan Event]struct{}
	down   bool // the server was unreachable at the last attempt
}

// NewWatcher returns a Watcher.
func NewWatcher(server *Server) *Watcher {
	return &Watcher{server: server, hubs: map[hubKey]*hub{}}
}

// Subscribe returns a channel of events for acct, until ctx ends. The channel is closed then,
// and also after EventPasswordChanged.
func (w *Watcher) Subscribe(ctx context.Context, acct Account) <-chan Event {
	key := hubKey{acct.Name, sha256.Sum256([]byte(acct.Password))}
	ch := make(chan Event, 8)

	w.mu.Lock()
	h := w.hubs[key]
	if h == nil {
		hubCtx, cancel := context.WithCancel(context.Background())
		h = &hub{cancel: cancel, subs: map[chan Event]struct{}{}}
		w.hubs[key] = h
		go w.run(hubCtx, key, h, acct)
	}
	h.subs[ch] = struct{}{}
	if h.down {
		ch <- EventUnreachable
	}
	w.mu.Unlock()

	context.AfterFunc(ctx, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if _, ok := h.subs[ch]; !ok {
			return // already closed by the hub
		}
		delete(h.subs, ch)
		close(ch)
		if len(h.subs) == 0 && w.hubs[key] == h {
			delete(w.hubs, key)
			h.cancel()
		}
	})
	return ch
}

// send delivers e to every subscriber. A subscriber too slow to take it misses it; the next
// event, or the browser's own periodic refresh, catches it up.
func (w *Watcher) send(h *hub, e Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch e {
	case EventUnreachable:
		if h.down {
			return
		}
		h.down = true
	case EventReachable:
		if !h.down {
			return
		}
		h.down = false
	}
	h.tell(e)
}

// tell offers e to every subscriber without waiting; w.mu must be held.
func (h *hub) tell(e Event) {
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// end closes every subscriber after a final event and retires the hub.
func (w *Watcher) end(key hubKey, h *hub, e Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	h.tell(e)
	for ch := range h.subs {
		close(ch)
		delete(h.subs, ch)
	}
	if w.hubs[key] == h {
		delete(w.hubs, key)
	}
	h.cancel()
}

func (w *Watcher) run(ctx context.Context, key hubKey, h *hub, acct Account) {
	for ctx.Err() == nil {
		err := w.watch(ctx, h, acct)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, ErrRefused):
			w.end(key, h, EventPasswordChanged)
			return
		case err != nil:
			log.Printf("watch %s: %v", acct.Name, err)
			w.send(h, EventUnreachable)
		default:
			// A planned fresh login: no pause, and news that came in between the two
			// connections is announced, since neither of them saw it.
			w.send(h, EventInbox)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryAfter):
		}
	}
}

// watch logs in, waits in IDLE on the Inbox, and reports changes until the connection fails or
// ctx ends.
func (w *Watcher) watch(ctx context.Context, h *hub, acct Account) error {
	changed := make(chan struct{}, 1)
	poke := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	news := &imapclient.UnilateralDataHandler{
		Mailbox: func(*imapclient.UnilateralDataMailbox) { poke() },
		Expunge: func(uint32) { poke() },
		Fetch:   func(*imapclient.FetchMessageData) { poke() },
	}
	loginCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	c, err := w.server.login(loginCtx, acct.ClientIP, acct.Name, acct.Password, news)
	cancel()
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		return err
	}
	// Back after an outage: whatever happened meanwhile is news.
	w.send(h, EventReachable)
	// Dovecot keeps a logged-in connection working after a password change, so the watcher
	// logs in again now and then, as the pool does, to find out.
	relogin := time.Now().Add(maxAge)

	for {
		if time.Now().After(relogin) {
			return nil
		}
		idle, err := c.Idle()
		if err != nil {
			return err
		}
		restart := time.NewTimer(idleRestart)
		news := false
		select {
		case <-ctx.Done():
		case <-c.Closed():
		case <-changed:
			news = true
		case <-restart.C:
		}
		restart.Stop()
		if ctx.Err() != nil {
			idle.Close()
			return nil
		}
		if err := idle.Close(); err != nil {
			return err
		}
		if err := idle.Wait(); err != nil {
			return err
		}
		if news {
			// Let a burst of changes (a delivery, then its flags) arrive before telling anyone.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(200 * time.Millisecond):
			}
			select {
			case <-changed:
			default:
			}
			w.send(h, EventInbox)
		}
	}
}
