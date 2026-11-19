package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeSMTP is a submission server on implicit TLS. It offers SIZE and XCLIENT, accepts the
// password "pw", refuses recipients listed in reject, and records what it was told.
type fakeSMTP struct {
	mu     sync.Mutex
	lines  []string
	data   string
	reject map[string]string
}

func (f *fakeSMTP) said() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

func startSMTP(t *testing.T, reject map[string]string) (*Submitter, *fakeSMTP) {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	cert := srv.TLS.Certificates[0]
	srv.Close()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeSMTP{reject: reject}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	sub := &Submitter{Addr: ln.Addr().String(), TLS: &tls.Config{InsecureSkipVerify: true}, implicitTLS: true}
	return sub, f
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(s string) { conn.Write([]byte(s + "\r\n")) }
	say("220 fake ESMTP")
	inData := false
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				f.mu.Lock()
				f.data = data.String()
				f.mu.Unlock()
				say("250 2.0.0 Ok: queued")
				continue
			}
			data.WriteString(line + "\n")
			continue
		}
		f.mu.Lock()
		f.lines = append(f.lines, line)
		f.mu.Unlock()
		verb := strings.ToUpper(strings.Fields(line + " x")[0])
		switch {
		case verb == "EHLO":
			say("250-fake\r\n250-SIZE 1000\r\n250-XCLIENT ADDR NAME\r\n250 AUTH PLAIN")
		case verb == "XCLIENT":
			say("220 fake ESMTP again")
		case verb == "AUTH":
			if strings.HasSuffix(line, "AGFAZXhhbXBsZS50ZXN0AHB3") { // \0a@example.test\0pw
				say("235 2.7.0 Authentication successful")
			} else {
				say("535 5.7.8 Error: authentication failed")
			}
		case verb == "MAIL":
			say("250 2.1.0 Ok")
		case verb == "RCPT":
			to := strings.Trim(strings.TrimPrefix(strings.ToUpper(line), "RCPT TO:"), "<>")
			if msg, ok := f.reject[strings.ToLower(to)]; ok {
				say(msg)
			} else {
				say("250 2.1.5 Ok")
			}
		case verb == "DATA":
			inData = true
			say("354 End data with <CR><LF>.<CR><LF>")
		case verb == "QUIT":
			say("221 2.0.0 Bye")
			return
		default:
			say("502 5.5.2 Error: command not recognized")
		}
	}
}

func TestSubmitConversation(t *testing.T) {
	sub, f := startSMTP(t, map[string]string{"nobody@example.test": "550 5.1.1 <nobody@example.test>: Recipient address rejected: User unknown"})
	submit := sub

	sending := false
	msg := []byte("Subject: hi\r\n\r\n.leading dot\r\nbody\r\n")
	err := submit.Send(context.Background(), ip, "a@example.test", "pw", Envelope{From: "a@example.test", To: []string{"b@example.test"}}, msg, func() { sending = true })
	if err != nil {
		t.Fatal(err)
	}
	if !sending {
		t.Error("sending was not called before the data")
	}
	lines := f.said()
	if len(lines) < 4 || !strings.HasPrefix(lines[1], "XCLIENT ADDR=203.0.113.7") || !strings.HasPrefix(lines[3], "AUTH PLAIN") {
		t.Errorf("conversation: %q", lines)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "MAIL FROM:<a@example.test> SIZE=") {
		t.Errorf("no SIZE on MAIL FROM: %q", lines)
	}
	if f.data != "Subject: hi\n\n..leading dot\nbody\n" {
		t.Errorf("data %q", f.data)
	}

	var rejected *Rejected
	err = submit.Send(context.Background(), ip, "a@example.test", "pw", Envelope{From: "a@example.test", To: []string{"nobody@example.test"}}, msg, func() {})
	if !errors.As(err, &rejected) || rejected.Code != 550 || !strings.Contains(rejected.Text, "User unknown") {
		t.Errorf("got %v, want the server's rejection", err)
	}

	if err := submit.Send(context.Background(), ip, "a@example.test", "wrong", Envelope{From: "a@example.test", To: []string{"b@example.test"}}, msg, func() {}); !errors.Is(err, ErrRefused) {
		t.Errorf("wrong password: got %v", err)
	}

	if limit := submit.SizeLimit(context.Background()); limit != 1000 {
		t.Errorf("size limit %d", limit)
	}
}
