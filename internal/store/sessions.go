package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound means no row matched.
var ErrNotFound = errors.New("not found")

// Session is a row of the sessions table.
type Session struct {
	ID        string
	Account   string
	Sealed    []byte
	Method    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateSession stores a new session.
func (s *Store) CreateSession(ctx context.Context, ses Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, account, sealed, method, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		ses.ID, ses.Account, ses.Sealed, ses.Method, ses.CreatedAt.Unix(), ses.ExpiresAt.Unix())
	return err
}

// Session returns the session with id, or ErrNotFound.
func (s *Store) Session(ctx context.Context, id string) (Session, error) {
	ses := Session{ID: id}
	var created, expires int64
	err := s.db.QueryRowContext(ctx,
		`SELECT account, sealed, method, created_at, expires_at FROM sessions WHERE id = ?`, id,
	).Scan(&ses.Account, &ses.Sealed, &ses.Method, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	ses.CreatedAt, ses.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
	return ses, err
}

// DeleteSession removes one session. Removing one that does not exist is not an error.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteAccountSessions removes every session of account.
func (s *Store) DeleteAccountSessions(ctx context.Context, account string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE account = ?`, account)
	return err
}

// DeleteExpiredSessions removes sessions that expired at or before now.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix())
	return err
}
