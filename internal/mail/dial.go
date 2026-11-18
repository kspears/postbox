// Package mail talks to the mail server: IMAP for everything stored, submission for sending.
package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

// ErrUnreachable means the mail server could not be reached or did not answer properly:
// network, TLS, certificate, or server-side failure. It never means a wrong password.
var ErrUnreachable = errors.New("mail server unreachable")

// ErrRefused means the mail server refused the address and password.
var ErrRefused = errors.New("mail server refused the password")

// Server is where the mail server is and how its certificate is checked.
type Server struct {
	IMAPAddr string
	TLS      *tls.Config
}

// NewServer returns the Server for imapAddr. caFile, when set, adds certificate authorities to
// the system's; certificates are always verified.
func NewServer(imapAddr, caFile string) (*Server, error) {
	host, _, err := net.SplitHostPort(imapAddr)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("POSTBOX_CA_FILE: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("POSTBOX_CA_FILE: no PEM certificates in %s", caFile)
		}
		cfg.RootCAs = pool
	}
	return &Server{IMAPAddr: imapAddr, TLS: cfg}, nil
}

// Login opens an IMAP connection on behalf of the person at clientIP and logs in. The error is
// ErrRefused or wraps ErrUnreachable.
func (s *Server) Login(ctx context.Context, clientIP netip.Addr, account, password string) (*imapclient.Client, error) {
	c, err := s.dial(ctx, clientIP)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if err := waitCtx(ctx, c, c.Login(account, password).Wait); err != nil {
		c.Close()
		var imapErr *imap.Error
		if errors.As(err, &imapErr) && imapErr.Type == imap.StatusResponseTypeNo && !serverSide(imapErr.Code) {
			return nil, ErrRefused
		}
		return nil, fmt.Errorf("%w: login: %v", ErrUnreachable, err)
	}
	return c, nil
}

// Logout ends a session politely, giving the server five seconds to answer, and closes it.
func (s *Server) Logout(c *imapclient.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	waitCtx(ctx, c, c.Logout().Wait)
	c.Close()
}

// serverSide reports response codes that blame the server, not the credentials (RFC 5530).
func serverSide(code imap.ResponseCode) bool {
	switch code {
	case imap.ResponseCodeUnavailable, imap.ResponseCodeServerBug, imap.ResponseCodeContactAdmin,
		imap.ResponseCodeLimit, imap.ResponseCodeInUse:
		return true
	}
	return false
}

// dial connects over TLS and tells the server who the person is before handing the connection
// to the IMAP library. The library's ID command has no room for x-originating-ip, so Postbox
// sends that one command itself. A server that trusts Postbox's address then applies its own
// login penalties and logs to the person's address instead of Postbox's.
func (s *Server) dial(ctx context.Context, clientIP netip.Addr) (*imapclient.Client, error) {
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: s.TLS}
	conn, err := dialer.DialContext(ctx, "tcp", s.IMAPAddr)
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	conn.SetDeadline(deadline)

	r := bufio.NewReaderSize(conn, 8192)
	greeting, err := readLine(r)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("greeting: %w", err)
	}
	if !strings.HasPrefix(greeting, "* OK") {
		conn.Close()
		return nil, fmt.Errorf("greeting: %q", strings.TrimSpace(greeting))
	}
	if err := sendID(conn, r, clientIP); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})

	// The library reads the greeting again from the replayed bytes, then whatever follows.
	replay := &replayConn{Conn: conn, r: io.MultiReader(strings.NewReader(greeting), r)}
	return imapclient.New(replay, &imapclient.Options{
		// Without a charset reader, encoded subjects and names in anything but UTF-8 and
		// ASCII come out undecoded.
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}), nil
}

// sendID sends `ID` with the client's address and waits for the server's tagged answer. A
// server without ID support answers BAD, which is fine: it just does not learn the address.
func sendID(w io.Writer, r *bufio.Reader, clientIP netip.Addr) error {
	// Without its zone (which may hold any text), an address prints as digits, dots and colons
	// only, so it is safe inside a quoted string.
	cmd := fmt.Sprintf("pbid ID (\"name\" \"Postbox\" \"x-originating-ip\" \"%s\")\r\n", clientIP.Unmap().WithZone(""))
	if _, err := io.WriteString(w, cmd); err != nil {
		return fmt.Errorf("ID: %w", err)
	}
	for range 20 {
		line, err := readLine(r)
		if err != nil {
			return fmt.Errorf("ID: %w", err)
		}
		if strings.HasPrefix(line, "pbid ") {
			return nil
		}
	}
	return errors.New("ID: no answer after 20 lines")
}

// readLine reads one CRLF-terminated line; r's buffer size is the longest line accepted.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", errors.New("line too long")
	}
	return string(line), err
}

// replayConn is a connection whose first reads come from bytes already read off it.
type replayConn struct {
	net.Conn
	r io.Reader
}

func (c *replayConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// waitCtx runs wait, but gives up when ctx ends by closing the connection under it.
func waitCtx(ctx context.Context, c *imapclient.Client, wait func() error) error {
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	err := wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
