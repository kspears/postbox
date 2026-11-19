package store

import (
	"context"
	"net/mail"
	"strings"
	"time"
)

// Address is a person's display name and email address.
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// CollectAddresses remembers people as addresses account has sent mail to, as of at. An address
// seen again counts one more use, keeps the later time and takes the new name when it has one
// and is not older than the name already kept. Entries without a valid address are skipped.
func (s *Store) CollectAddresses(ctx context.Context, account string, people []Address, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range people {
		email := strings.ToLower(strings.TrimSpace(p.Email))
		if _, err := mail.ParseAddress(email); err != nil {
			continue
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO collected_addresses (account, email, name, used_at, uses) VALUES (?, ?, ?, ?, 1)
			 ON CONFLICT (account, email) DO UPDATE SET
			   name = CASE WHEN excluded.name <> '' AND (excluded.used_at >= used_at OR name = '') THEN excluded.name ELSE name END,
			   used_at = max(used_at, excluded.used_at),
			   uses = uses + 1`,
			account, email, strings.TrimSpace(p.Name), at.Unix())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// likeEscaper escapes what LIKE treats as special, with backslash as the escape character.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// MatchAddresses returns up to limit of account's collected addresses whose name or address
// starts with prefix, ignoring case, the most used first. An empty prefix matches nothing.
func (s *Store) MatchAddresses(ctx context.Context, account, prefix string, limit int) ([]Address, error) {
	found := []Address{}
	if prefix == "" {
		return found, nil
	}
	pattern := likeEscaper.Replace(prefix) + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, email FROM collected_addresses
		 WHERE account = ? AND (email LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\')
		 ORDER BY uses DESC, used_at DESC, email LIMIT ?`,
		account, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Address
		if err := rows.Scan(&a.Name, &a.Email); err != nil {
			return nil, err
		}
		found = append(found, a)
	}
	return found, rows.Err()
}
