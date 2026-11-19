package store

import (
	"context"
	"strings"
)

// AllowImages records that account always loads remote images from sender.
func (s *Store) AllowImages(ctx context.Context, account, sender string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO image_allow (account, sender) VALUES (?, ?) ON CONFLICT DO NOTHING`, account, sender)
	return err
}

// ImagesAllowed returns the senders, of those given, whose remote images account always loads.
func (s *Store) ImagesAllowed(ctx context.Context, account string, senders []string) (map[string]bool, error) {
	allowed := map[string]bool{}
	if len(senders) == 0 {
		return allowed, nil
	}
	args := []any{account}
	for _, sender := range senders {
		args = append(args, sender)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT sender FROM image_allow WHERE account = ? AND sender IN (?`+strings.Repeat(",?", len(senders)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sender string
		if err := rows.Scan(&sender); err != nil {
			return nil, err
		}
		allowed[sender] = true
	}
	return allowed, rows.Err()
}
