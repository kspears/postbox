// Command postbox is the Postbox web mail server.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

Settings come from POSTBOX_* environment variables; see the README.`

func main() {
	if len(os.Args) != 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err := serve(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
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
	out, err := outbox.New(ctx, st, pool, submit)
	if err != nil {
		return fmt.Errorf("POSTBOX_STATE: %w", err)
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("cannot listen on POSTBOX_LISTEN: %w", err)
	}
	srv := &http.Server{
		Handler: api.NewHandler(web.Dist(), api.Deps{
			Sessions:       sessions,
			Auth:           auth.New(st, clk, server),
			Mail:           pool,
			Submit:         submit,
			Outbox:         out,
			Store:          st,
			Watch:          mail.NewWatcher(server),
			Stopping:       ctx,
			Clock:          clk,
			Origin:         cfg.PublicURL.String(),
			ClientIPHeader: cfg.ClientIPHeader,
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
