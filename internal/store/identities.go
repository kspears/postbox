package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Identity is an address the person sends as, with its signature.
type Identity struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Signature string `json:"signature"` // sanitised HTML
}

// ErrLoginIdentity means the change would remove the login address or change it.
var ErrLoginIdentity = errors.New("the login address stays an identity")

// Identities returns account's identities, the login address first. The first read makes the
// login identity.
func (s *Store) Identities(ctx context.Context, account string) ([]Identity, error) {
	list, err := s.identities(ctx, account)
	if err != nil || len(list) > 0 {
		return list, err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO identities (account, email, position) SELECT ?, ?, 0
		 WHERE NOT EXISTS (SELECT 1 FROM identities WHERE account = ?)`, account, account, account)
	if err != nil {
		return nil, err
	}
	return s.identities(ctx, account)
}

func (s *Store) identities(ctx context.Context, account string) ([]Identity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, email, signature FROM identities WHERE account = ? ORDER BY position, id`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Identity
	for rows.Next() {
		var id Identity
		if err := rows.Scan(&id.ID, &id.Name, &id.Email, &id.Signature); err != nil {
			return nil, err
		}
		list = append(list, id)
	}
	return list, rows.Err()
}

// SaveIdentity adds id (ID 0) or changes one of account's identities, and returns it as stored.
// The login identity keeps its address.
func (s *Store) SaveIdentity(ctx context.Context, account string, id Identity) (Identity, error) {
	if id.ID == 0 {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO identities (account, name, email, signature, position)
			 VALUES (?, ?, ?, ?, (SELECT coalesce(max(position), 0) + 1 FROM identities WHERE account = ?))`,
			account, id.Name, id.Email, id.Signature, account)
		if err != nil {
			return Identity{}, err
		}
		id.ID, err = res.LastInsertId()
		return id, err
	}
	var email string
	err := s.db.QueryRowContext(ctx, `SELECT email FROM identities WHERE account = ? AND id = ?`, account, id.ID).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrNotFound
	}
	if err != nil {
		return Identity{}, err
	}
	if strings.EqualFold(email, account) && !strings.EqualFold(id.Email, account) {
		return Identity{}, ErrLoginIdentity
	}
	_, err = s.db.ExecContext(ctx, `UPDATE identities SET name = ?, email = ?, signature = ? WHERE account = ? AND id = ?`,
		id.Name, id.Email, id.Signature, account, id.ID)
	return id, err
}

// DeleteIdentity removes one of account's identities other than the login address.
func (s *Store) DeleteIdentity(ctx context.Context, account string, id int64) error {
	var email string
	err := s.db.QueryRowContext(ctx, `SELECT email FROM identities WHERE account = ? AND id = ?`, account, id).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(email, account) {
		return ErrLoginIdentity
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM identities WHERE account = ? AND id = ?`, account, id)
	return err
}
