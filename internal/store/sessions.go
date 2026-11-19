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
	// ConfirmedUntil is until when a request carrying the confirmation token whose SHA-256 is
	// ConfirmHash may change passkeys without the password again.
	ConfirmedUntil time.Time
	ConfirmHash    []byte
}

// CreateSession stores a new session, and deletes sessions that expired before it.
func (s *Store) CreateSession(ctx context.Context, ses Session) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, ses.CreatedAt.Unix()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, account, sealed, method, created_at, expires_at, confirmed_until, confirm_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ses.ID, ses.Account, ses.Sealed, ses.Method, ses.CreatedAt.Unix(), ses.ExpiresAt.Unix(), ses.ConfirmedUntil.Unix(), ses.ConfirmHash)
	return err
}

// Session returns the session with id, or ErrNotFound.
func (s *Store) Session(ctx context.Context, id string) (Session, error) {
	ses := Session{ID: id}
	var created, expires, confirmed int64
	err := s.db.QueryRowContext(ctx,
		`SELECT account, sealed, method, created_at, expires_at, confirmed_until, confirm_hash FROM sessions WHERE id = ?`, id,
	).Scan(&ses.Account, &ses.Sealed, &ses.Method, &created, &expires, &confirmed, &ses.ConfirmHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	ses.CreatedAt, ses.ExpiresAt, ses.ConfirmedUntil = time.Unix(created, 0), time.Unix(expires, 0), time.Unix(confirmed, 0)
	return ses, err
}

// ConfirmSession lets requests of session id that carry the token hashing to hash change
// passkeys without the password again until until. It is ErrNotFound when the session is gone.
func (s *Store) ConfirmSession(ctx context.Context, id string, hash []byte, until time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET confirmed_until = ?, confirm_hash = ? WHERE id = ?`, until.Unix(), hash, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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
