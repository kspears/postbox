package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kspears/postbox/internal/mail/mailtest"
)

// fakeIMAP answers each command verb with the reply in answer, where "TAG" stands for the
// command's tag; unknown verbs get BAD. lines returns every command received so far.
func fakeIMAP(t *testing.T, greeting string, answer map[string]string) (s *Server, lines func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	addr := mailtest.Listen(t, greeting, func(tag, verb, line string) (string, bool) {
		mu.Lock()
		seen = append(seen, line)
		mu.Unlock()
		reply, ok := answer[verb]
		if !ok {
			reply = "TAG BAD unexpected\r\n"
		}
		return strings.ReplaceAll(reply, "TAG", tag), verb == "LOGOUT"
	})
	return &Server{IMAPAddr: addr, TLS: &tls.Config{InsecureSkipVerify: true}}, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

var ip = netip.MustParseAddr("203.0.113.7")

func TestIDDropsTheZone(t *testing.T) {
	s, seen := fakeIMAP(t, "* OK ready\r\n", map[string]string{"ID": "TAG OK\r\n", "LOGIN": "TAG NO no\r\n"})
	zoned := netip.MustParseAddr(`fe80::1%x" "x-forward-user" "admin`)
	s.Login(context.Background(), zoned, "a", "b")
	if lines := seen(); len(lines) == 0 || lines[0] != `pbid ID ("name" "Postbox" "x-originating-ip" "fe80::1")` {
		t.Errorf("server saw %q", lines)
	}
}

func TestIDIsSentBeforeLogin(t *testing.T) {
	s, seen := fakeIMAP(t, "* OK [CAPABILITY IMAP4rev1 ID AUTH=PLAIN] ready\r\n", map[string]string{
		"ID":     "* ID (\"name\" \"fake\")\r\nTAG OK ID done\r\n",
		"LOGIN":  "TAG OK [CAPABILITY IMAP4rev1 ID] logged in\r\n",
		"LOGOUT": "* BYE\r\nTAG OK bye\r\n",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := s.Login(ctx, ip, "a@example.test", "pw")
	if err != nil {
		t.Fatal(err)
	}
	c.Logout().Wait()
	c.Close()

	lines := seen()
	if len(lines) < 2 {
		t.Fatalf("server saw %q", lines)
	}
	if want := `pbid ID ("name" "Postbox" "x-originating-ip" "203.0.113.7")`; lines[0] != want {
		t.Errorf("first command %q, want %q", lines[0], want)
	}
	if !strings.Contains(lines[1], "LOGIN") {
		t.Errorf("second command %q, want LOGIN", lines[1])
	}
}

func TestLoginOutcomes(t *testing.T) {
	tests := []struct {
		name  string
		login string
		want  error
	}{
		{"refused", "TAG NO [AUTHENTICATIONFAILED] Authentication failed.\r\n", ErrRefused},
		{"refused without a code", "TAG NO bad password\r\n", ErrRefused},
		{"server trouble", "TAG NO [UNAVAILABLE] Temporary failure\r\n", ErrUnreachable},
		{"protocol error", "TAG BAD what\r\n", ErrUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := fakeIMAP(t, "* OK ready\r\n", map[string]string{
				"ID":    "TAG OK\r\n",
				"LOGIN": tt.login,
			})
			_, err := s.Login(context.Background(), ip, "a@example.test", "pw")
			if !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestServerWithoutIDStillLogsIn(t *testing.T) {
	s, _ := fakeIMAP(t, "* OK ready\r\n", map[string]string{
		"LOGIN": "TAG OK logged in\r\n",
	})
	c, err := s.Login(context.Background(), ip, "a@example.test", "pw")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestUnreachable(t *testing.T) {
	t.Run("greeting is BYE", func(t *testing.T) {
		s, _ := fakeIMAP(t, "* BYE too busy\r\n", nil)
		if _, err := s.Login(context.Background(), ip, "a", "b"); !errors.Is(err, ErrUnreachable) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("nothing listening", func(t *testing.T) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		addr := ln.Addr().String()
		ln.Close()
		s := &Server{IMAPAddr: addr, TLS: &tls.Config{}}
		if _, err := s.Login(context.Background(), ip, "a", "b"); !errors.Is(err, ErrUnreachable) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("untrusted certificate", func(t *testing.T) {
		s, _ := fakeIMAP(t, "* OK ready\r\n", nil)
		s.TLS = &tls.Config{ServerName: "example.test"}
		if _, err := s.Login(context.Background(), ip, "a", "b"); !errors.Is(err, ErrUnreachable) {
			t.Errorf("got %v", err)
		}
	})
}
