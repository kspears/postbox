// Package mailtest runs scripted IMAP servers for tests.
package mailtest

import (
	"bufio"
	"crypto/tls"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
)

// Respond answers one command line (without CRLF) with the full reply to send, CRLFs included.
// Returning close as true ends the connection after the reply.
type Respond func(tag, verb, line string) (reply string, close bool)

// Listen starts a TLS IMAP server that sends greeting to every connection and answers each
// command with respond. It returns the server's address; clients must skip certificate checks.
func Listen(t *testing.T, greeting string, respond Respond) string {
	t.Helper()
	// httptest makes a throwaway certificate; the HTTP server itself is not used.
	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	cert := srv.TLS.Certificates[0]
	srv.Close()

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(conn, greeting, respond)
		}
	}()
	return ln.Addr().String()
}

func serve(conn net.Conn, greeting string, respond Respond) {
	defer conn.Close()
	conn.Write([]byte(greeting))
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		tag, rest, _ := strings.Cut(line, " ")
		verb, _, _ := strings.Cut(rest, " ")
		reply, done := respond(tag, strings.ToUpper(verb), line)
		conn.Write([]byte(reply))
		if done {
			return
		}
	}
}
