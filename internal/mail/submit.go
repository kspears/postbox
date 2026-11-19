package mail

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Rejected is the submission server's refusal of a message, with its own words.
type Rejected struct {
	Code int
	Text string
}

func (r *Rejected) Error() string { return fmt.Sprintf("%d %s", r.Code, r.Text) }

// Submitter sends mail through the submission server.
type Submitter struct {
	Addr string
	TLS  *tls.Config
	// implicitTLS: the connection is TLS from the first byte (port 465), not upgraded by STARTTLS.
	implicitTLS bool
}

// NewSubmitter returns a Submitter for addr. Port 465 speaks TLS from the first byte; any other
// port must offer STARTTLS, which is required. Certificates are checked as for IMAP.
func NewSubmitter(addr string, imap *Server) (*Submitter, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	cfg := imap.TLS.Clone()
	cfg.ServerName = host
	_, port, _ := net.SplitHostPort(addr)
	return &Submitter{Addr: addr, TLS: cfg, implicitTLS: port == "465"}, nil
}

// Envelope is who a message is from and to, as the server sees it (Bcc included).
type Envelope struct {
	From string
	To   []string
}

// session is one conversation with the submission server, ready for MAIL FROM.
type session struct {
	conn net.Conn
	text *textproto.Conn
	ext  map[string]string // EHLO extensions, upper case, with their parameters
}

func (s *session) close() { s.text.Close() }

func (s *session) cmd(expect int, format string, args ...any) (int, string, error) {
	id, err := s.text.Cmd(format, args...)
	if err != nil {
		return 0, "", err
	}
	s.text.StartResponse(id)
	defer s.text.EndResponse(id)
	return s.text.ReadResponse(expect)
}

func (s *session) ehlo() error {
	_, msg, err := s.cmd(250, "EHLO postbox")
	if err != nil {
		return err
	}
	s.ext = map[string]string{}
	for _, line := range strings.Split(msg, "\n")[1:] {
		name, params, _ := strings.Cut(line, " ")
		s.ext[strings.ToUpper(name)] = params
	}
	return nil
}

// open connects, secures the connection, says who the person is (XCLIENT, when the server
// lets Postbox) and logs in.
func (sub *Submitter) open(ctx context.Context, clientIP netip.Addr, account, password string) (*session, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if sub.implicitTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: sub.TLS}).DialContext(ctx, "tcp", sub.Addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", sub.Addr)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Minute)
	}
	conn.SetDeadline(deadline)
	s := &session{conn: conn, text: textproto.NewConn(conn)}
	fail := func(err error) (*session, error) {
		s.close()
		var tpErr *textproto.Error
		if errors.As(err, &tpErr) {
			return nil, fmt.Errorf("%w: %d %s", ErrUnreachable, tpErr.Code, tpErr.Msg)
		}
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	if _, _, err := s.text.ReadResponse(220); err != nil {
		return fail(err)
	}
	if err := s.ehlo(); err != nil {
		return fail(err)
	}
	if !sub.implicitTLS {
		if _, ok := s.ext["STARTTLS"]; !ok {
			return fail(errors.New("the submission server does not offer STARTTLS"))
		}
		if _, _, err := s.cmd(220, "STARTTLS"); err != nil {
			return fail(err)
		}
		secure := tls.Client(conn, sub.TLS)
		if err := secure.HandshakeContext(ctx); err != nil {
			return fail(err)
		}
		s.conn, s.text = secure, textproto.NewConn(secure)
		if err := s.ehlo(); err != nil {
			return fail(err)
		}
	}
	// A server that trusts Postbox's address takes the person's own for its logs and limits.
	// After XCLIENT the session starts again, with a new greeting.
	if params, ok := s.ext["XCLIENT"]; ok && strings.Contains(strings.ToUpper(params), "ADDR") && clientIP.IsValid() {
		addr := clientIP.Unmap().WithZone("")
		value := addr.String()
		if addr.Is6() {
			value = "IPV6:" + value
		}
		if _, _, err := s.cmd(220, "XCLIENT ADDR=%s", value); err != nil {
			return fail(err)
		}
		if err := s.ehlo(); err != nil {
			return fail(err)
		}
	}
	plain := base64.StdEncoding.EncodeToString([]byte("\x00" + account + "\x00" + password))
	if code, msg, err := s.cmd(235, "AUTH PLAIN %s", plain); err != nil {
		s.close()
		if code == 535 {
			return nil, ErrRefused
		}
		return nil, fmt.Errorf("%w: %d %s", ErrUnreachable, code, msg)
	}
	return s, nil
}

// SizeLimit returns the largest message the server accepts, in bytes, or 0 when it states none.
func (sub *Submitter) SizeLimit(ctx context.Context, clientIP netip.Addr, account, password string) (int64, error) {
	s, err := sub.open(ctx, clientIP, account, password)
	if err != nil {
		return 0, err
	}
	defer s.close()
	s.cmd(221, "QUIT")
	n, _ := strconv.ParseInt(strings.TrimSpace(s.ext["SIZE"]), 10, 64)
	return n, nil
}

// Send submits a message. Before the first byte of the message is sent, sending is called: from
// then on Postbox cannot tell whether a failure means the message went out. The error is a
// *Rejected when the server refused the message, ErrRefused when it refused the password, or
// wraps ErrUnreachable.
func (sub *Submitter) Send(ctx context.Context, clientIP netip.Addr, account, password string, env Envelope, msg []byte, sending func()) error {
	s, err := sub.open(ctx, clientIP, account, password)
	if err != nil {
		return err
	}
	defer s.close()
	reject := func(code int, text string, err error) error {
		var tpErr *textproto.Error
		if errors.As(err, &tpErr) {
			return &Rejected{Code: tpErr.Code, Text: tpErr.Msg}
		}
		if code != 0 {
			return &Rejected{Code: code, Text: text}
		}
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	from := "MAIL FROM:<%s>"
	args := []any{env.From}
	if _, ok := s.ext["SIZE"]; ok {
		from += " SIZE=%d"
		args = append(args, len(msg))
	}
	if code, text, err := s.cmd(250, from, args...); err != nil {
		return reject(code, text, err)
	}
	for _, to := range env.To {
		if code, text, err := s.cmd(25, "RCPT TO:<%s>", to); err != nil {
			return reject(code, text, err)
		}
	}
	if code, text, err := s.cmd(354, "DATA"); err != nil {
		return reject(code, text, err)
	}
	sending()
	w := s.text.DotWriter()
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if code, text, err := s.text.ReadResponse(250); err != nil {
		return reject(code, text, err)
	}
	s.cmd(221, "QUIT")
	return nil
}
