package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Setting reads account's value for key into v. It leaves v alone when nothing is stored.
func (s *Store) Setting(ctx context.Context, account, key string, v any) error {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM account_settings WHERE account = ? AND key = ?`, account, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}

// SetSetting stores v as account's value for key.
func (s *Store) SetSetting(ctx context.Context, account, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO account_settings (account, key, value) VALUES (?, ?, ?)
		 ON CONFLICT (account, key) DO UPDATE SET value = excluded.value`,
		account, key, string(raw))
	return err
}
