package store

import "context"

// AllowImages records that account always loads remote images from sender.
func (s *Store) AllowImages(ctx context.Context, account, sender string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO image_allow (account, sender) VALUES (?, ?) ON CONFLICT DO NOTHING`, account, sender)
	return err
}

// ImagesAllowed returns which of senders account always loads remote images from.
func (s *Store) ImagesAllowed(ctx context.Context, account string, senders []string) (map[string]bool, error) {
	allowed := map[string]bool{}
	for _, sender := range senders {
		var n int
		err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM image_allow WHERE account = ? AND sender = ?`, account, sender).Scan(&n)
		if err != nil {
			return nil, err
		}
		allowed[sender] = n > 0
	}
	return allowed, nil
}
