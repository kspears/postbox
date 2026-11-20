// Command postbox is the Postbox web mail server.
package main

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kspears/postbox/internal/addressbook"
	"github.com/kspears/postbox/internal/api"
	"github.com/kspears/postbox/internal/auth"
	"github.com/kspears/postbox/internal/clock"
	"github.com/kspears/postbox/internal/config"
	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/outbox"
	"github.com/kspears/postbox/internal/session"
	"github.com/kspears/postbox/internal/store"
	"github.com/kspears/postbox/web"
)

const usage = `usage: postbox serve
       postbox passkeys clear <address>

Settings come from POSTBOX_* environment variables; see the README.`

func main() {
	var err error
	switch args := os.Args[1:]; {
	case len(args) == 1 && args[0] == "serve":
		err = serve()
	case len(args) == 3 && args[0] == "passkeys" && args[1] == "clear":
		err = clearPasskeys(args[2])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// clearPasskeys removes every passkey of the account at address, ends its passkey sessions and
// turns "Require passkey" off, for a person who has lost theirs. It needs only POSTBOX_STATE.
func clearPasskeys(address string) error {
	ctx := context.Background()
	st, err := store.Open(ctx, cmp.Or(os.Getenv("POSTBOX_STATE"), config.DefaultState))
	if err != nil {
		return fmt.Errorf("POSTBOX_STATE: %w", err)
	}
	defer st.Close()
	account := auth.Account(address)
	n, err := st.ClearPasskeys(ctx, account)
	if err != nil {
		return err
	}
	fmt.Printf("Cleared %d passkeys for %s. Require passkey is off.\n", n, account)
	return nil
}

func serve() error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	server, err := mail.NewServer(cfg.IMAPAddr, cfg.CAFile)
	if err != nil {
		return err
	}
	submit, err := mail.NewSubmitter(cfg.SMTPAddr, server)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.State, 0o700); err != nil {
		return fmt.Errorf("POSTBOX_STATE: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st, err := store.Open(ctx, cfg.State)
	if err != nil {
		return fmt.Errorf("POSTBOX_STATE: %w", err)
	}
	defer st.Close()

	clk := &clock.Clock{}
	sessions := session.NewManager(st, clk, cfg.PublicURL.Scheme == "https")
	pool := mail.NewPool(ctx, server, clk.Now)
	book := addressbook.New(st, pool)
	out, err := outbox.New(ctx, st, pool, submit, book)
	if err != nil {
		return fmt.Errorf("POSTBOX_STATE: %w", err)
	}

	authenticator, err := auth.New(st, clk, server, cfg.PublicURL, cfg.PasskeyKey)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("cannot listen on POSTBOX_LISTEN: %w", err)
	}
	srv := &http.Server{
		Handler: api.NewHandler(web.Dist(), api.Deps{
			Sessions:       sessions,
			Auth:           authenticator,
			Mail:           pool,
			Submit:         submit,
			Outbox:         out,
			Addresses:      book,
			Store:          st,
			Watch:          mail.NewWatcher(server),
			Stopping:       ctx,
			Clock:          clk,
			Origin:         cfg.PublicURL.String(),
			ClientIPHeader: cfg.ClientIPHeader,
			TrustedProxy:   cfg.TrustedProxy,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	log.Printf("listening on %s for %s", ln.Addr(), cfg.PublicURL)
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	// Let requests in flight finish before the process exits.
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(drain)
}
