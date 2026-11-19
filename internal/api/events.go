package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/session"
)

// events answers GET /api/events with a stream of server-sent events about the account: new or
// changed mail in the Inbox, and the mail server going away and coming back.
func (a *apiHandler) events(w http.ResponseWriter, r *http.Request, s *session.Session) {
	rc := http.NewResponseController(w)
	// The server's read timeout would otherwise end the stream after 30 seconds.
	if err := rc.SetReadDeadline(time.Time{}); err != nil {
		internalError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // tell proxies not to hold events back
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": hello\n\n")
	rc.Flush()

	// The stream ends with the request or with Postbox shutting down, whichever comes first.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(a.Stopping, cancel)
	defer stop()

	ch := a.Watch.Subscribe(ctx, a.account(r, s))
	// A comment now and then keeps proxies from closing a quiet stream.
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			// The session may have ended elsewhere (sign out everywhere, expiry); the stream and
			// its mail-server connection must not outlive it.
			if _, err := a.Sessions.FromRequest(r); err != nil {
				fmt.Fprint(w, "event: signed_out\ndata: {}\n\n")
				rc.Flush()
				return
			}
			fmt.Fprint(w, ": ping\n\n")
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e == mail.EventPasswordChanged {
				a.Sessions.End(context.WithoutCancel(ctx), s)
			}
			fmt.Fprintf(w, "event: %s\ndata: {}\n\n", e)
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
