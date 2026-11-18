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
	"github.com/kspears/postbox/internal/config"
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
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("cannot listen on POSTBOX_LISTEN: %w", err)
	}
	srv := &http.Server{
		Handler:           api.NewHandler(web.Dist()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
