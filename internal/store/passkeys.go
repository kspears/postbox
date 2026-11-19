package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// requirePasskey is the account_settings key of the "Require passkey" switch.
const requirePasskey = "require_passkey"

// How a session started: sessions.method.
const (
	MethodPassword = "password"
	MethodPasskey  = "passkey"
)

// What a passkey's password is sealed under.
const (
	LockPRF    = "prf"    // a key from the passkey's PRF output
	LockServer = "server" // a key from POSTBOX_PASSKEY_KEY, for a passkey without PRF
)

// Passkey is a row of the passkeys table.
type Passkey struct {
	ID         []byte
	Account    string
	UserHandle []byte
	Credential []byte
	Sealed     []byte
	Lock       string
	CreatedAt  time.Time
}

// AddPasskey stores a new passkey.
func (s *Store) AddPasskey(ctx context.Context, p Passkey) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO passkeys (id, account, user_handle, credential, sealed, lock, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Account, p.UserHandle, string(p.Credential), p.Sealed, p.Lock, p.CreatedAt.Unix())
	return err
}

// PasskeyByID returns the passkey with credential id, or ErrNotFound.
func (s *Store) PasskeyByID(ctx context.Context, id []byte) (Passkey, error) {
	rows, err := s.passkeys(ctx, `WHERE id = ?`, id)
	if err != nil {
		return Passkey{}, err
	}
	if len(rows) == 0 {
		return Passkey{}, ErrNotFound
	}
	return rows[0], nil
}

// Passkeys returns account's passkeys, oldest first.
func (s *Store) Passkeys(ctx context.Context, account string) ([]Passkey, error) {
	return s.passkeys(ctx, `WHERE account = ? ORDER BY created_at, rowid`, account)
}

func (s *Store) passkeys(ctx context.Context, where string, arg any) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, account, user_handle, credential, sealed, lock, created_at FROM passkeys `+where, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		var credential string
		var created int64
		if err := rows.Scan(&p.ID, &p.Account, &p.UserHandle, &credential, &p.Sealed, &p.Lock, &created); err != nil {
			return nil, err
		}
		p.Credential, p.CreatedAt = []byte(credential), time.Unix(created, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePasskeyCredential stores a passkey's credential after a sign-in: its counters change.
func (s *Store) UpdatePasskeyCredential(ctx context.Context, id, credential []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE passkeys SET credential = ? WHERE id = ?`, string(credential), id)
	return err
}

// UpdatePasskeySealed replaces the password stored for a passkey.
func (s *Store) UpdatePasskeySealed(ctx context.Context, id, sealed []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE passkeys SET sealed = ? WHERE id = ?`, sealed, id)
	return err
}

// RemovePasskey removes one of account's passkeys, and ends the account's passkey sessions
// other than keep (the one that asked): a passkey is removed because it was lost. Removing the
// last turns "Require passkey" off. It reports whether "Require passkey" is still on.
func (s *Store) RemovePasskey(ctx context.Context, account string, id []byte, keep string) (required bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM passkeys WHERE account = ? AND id = ?`, account, id)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, ErrNotFound
	}
	if err := endSessions(ctx, tx, account, MethodPasskey, keep); err != nil {
		return false, err
	}
	left, err := countPasskeys(ctx, tx, account)
	if err != nil {
		return false, err
	}
	if left == 0 {
		if err := clearRequire(ctx, tx, account); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) > 0 FROM account_settings WHERE account = ? AND key = ? AND value = 'true'`,
		account, requirePasskey).Scan(&required); err != nil {
		return false, err
	}
	return required, tx.Commit()
}

func countPasskeys(ctx context.Context, tx *sql.Tx, account string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM passkeys WHERE account = ?`, account).Scan(&n)
	return n, err
}

// ClearPasskeys removes every passkey of account, ends its passkey sessions, and turns
// "Require passkey" off. It returns how many passkeys there were.
func (s *Store) ClearPasskeys(ctx context.Context, account string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM passkeys WHERE account = ?`, account)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := endSessions(ctx, tx, account, MethodPasskey, ""); err != nil {
		return 0, err
	}
	if err := clearRequire(ctx, tx, account); err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}

// endSessions ends account's sessions started by method, other than keep.
func endSessions(ctx context.Context, tx *sql.Tx, account, method, keep string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE account = ? AND method = ? AND id != ?`, account, method, keep)
	return err
}

func clearRequire(ctx context.Context, tx *sql.Tx, account string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM account_settings WHERE account = ? AND key = ?`, account, requirePasskey)
	return err
}

// PasskeyRequired reports whether "Require passkey" is on for account.
func (s *Store) PasskeyRequired(ctx context.Context, account string) (bool, error) {
	var on bool
	err := s.Setting(ctx, account, requirePasskey, &on)
	return on, err
}

// ErrNoPasskey means "Require passkey" cannot be turned on: the account has no passkey.
var ErrNoPasskey = errors.New("no passkey")

// RequirePasskey turns "Require passkey" on or off. Turning it on needs a passkey, and ends the
// account's password sessions other than keep (the one that asked).
func (s *Store) RequirePasskey(ctx context.Context, account string, on bool, keep string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !on {
		if err := clearRequire(ctx, tx, account); err != nil {
			return err
		}
		return tx.Commit()
	}
	n, err := countPasskeys(ctx, tx, account)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNoPasskey
	}
	if err := setSetting(ctx, tx, account, requirePasskey, true); err != nil {
		return err
	}
	if err := endSessions(ctx, tx, account, MethodPassword, keep); err != nil {
		return err
	}
	return tx.Commit()
}
