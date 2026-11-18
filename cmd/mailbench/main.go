// Command mailbench measures the mail server Postbox will sit in front of. It is a development
// tool, not part of the product.
//
//	mailbench check              run every IMAP command Postbox relies on and report each one
//	mailbench generate -n 50000  fill a folder with generated conversations
//	mailbench run                time the operations behind the spec's speed targets
//
// It reads POSTBOX_IMAP_ADDR and POSTBOX_CA_FILE like Postbox does, and the account's password
// from SEED_PASSWORD. `just bench` runs it on the test stack's network.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

type target struct {
	addr     string
	tls      *tls.Config
	user     string
	password string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mailbench check|generate|run [flags]")
		os.Exit(2)
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	user := flags.String("user", "bench@example.test", "account to use")
	folder := flags.String("folder", "Bench", "folder to generate into or measure")
	n := flags.Int("n", 10000, "generate: number of messages")
	flags.Parse(os.Args[2:])

	t, err := targetFromEnv(*user)
	if err == nil {
		switch os.Args[1] {
		case "check":
			err = check(t)
		case "generate":
			err = generate(t, *folder, *n)
		case "run":
			err = run(t, *folder)
		default:
			err = fmt.Errorf("unknown command %q", os.Args[1])
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func targetFromEnv(user string) (target, error) {
	t := target{addr: os.Getenv("POSTBOX_IMAP_ADDR"), user: user, password: os.Getenv("SEED_PASSWORD"), tls: &tls.Config{}}
	if t.addr == "" || t.password == "" {
		return t, fmt.Errorf("POSTBOX_IMAP_ADDR and SEED_PASSWORD must be set; run this through `just bench`")
	}
	if ca := os.Getenv("POSTBOX_CA_FILE"); ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			return t, fmt.Errorf("POSTBOX_CA_FILE: %w", err)
		}
		t.tls.RootCAs = x509.NewCertPool()
		if !t.tls.RootCAs.AppendCertsFromPEM(pem) {
			return t, fmt.Errorf("POSTBOX_CA_FILE: no certificates in %s", ca)
		}
	}
	return t, nil
}

// The commands Postbox will send, shared so that check and run exercise the same ones.
var (
	everything     = &imap.SearchCriteria{}
	listWithCounts = &imap.ListOptions{ReturnSpecialUse: true, ReturnStatus: &imap.StatusOptions{NumMessages: true, NumUnseen: true}}
	threadRefs     = &imapclient.ThreadOptions{Algorithm: "REFS", SearchCriteria: everything}
	threadFallback = &imapclient.ThreadOptions{Algorithm: imap.ThreadReferences, SearchCriteria: everything}
	newestFirst    = &imapclient.SortOptions{SearchCriteria: everything, SortCriteria: []imapclient.SortCriterion{{Key: imapclient.SortKeyArrival, Reverse: true}}}
)

// dial connects without logging in.
func (t target) dial(handler *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	return imapclient.DialTLS(t.addr, &imapclient.Options{TLSConfig: t.tls, UnilateralDataHandler: handler})
}

func (t target) login(handler *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	c, err := t.dial(handler)
	if err != nil {
		return nil, err
	}
	if err := c.Login(t.user, t.password).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("login as %s: %w", t.user, err)
	}
	return c, nil
}
