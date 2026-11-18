package store

import (
	"context"
	"time"
)

// Blocked reports whether a block is in force for any of keys at now.
func (s *Store) Blocked(ctx context.Context, now time.Time, keys ...string) (bool, error) {
	for _, key := range keys {
		var n int
		err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM login_blocks WHERE key = ? AND until > ?`, key, now.Unix()).Scan(&n)
		if err != nil || n > 0 {
			return n > 0, err
		}
	}
	return false, nil
}

// FailureLimit is one throttle rule: Max failures within Window block Key for Block.
type FailureLimit struct {
	Key     string
	Account string // empty: count failures for every account from the IP
	Max     int
	Window  time.Duration
	Block   time.Duration
}

// RecordFailure stores a failed sign-in for account from ip, and adds a block for every limit
// the failure reaches. It forgets failures older than the longest window.
func (s *Store) RecordFailure(ctx context.Context, now time.Time, account, ip string, limits ...FailureLimit) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldest time.Time = now
	for _, l := range limits {
		if since := now.Add(-l.Window); since.Before(oldest) {
			oldest = since
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_failures WHERE at <= ?`, oldest.Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_blocks WHERE until <= ?`, now.Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO login_failures (account, ip, at) VALUES (?, ?, ?)`, account, ip, now.Unix()); err != nil {
		return err
	}
	for _, l := range limits {
		var n int
		since := now.Add(-l.Window).Unix()
		if l.Account == "" {
			err = tx.QueryRowContext(ctx, `SELECT count(*) FROM login_failures WHERE ip = ? AND at > ?`, ip, since).Scan(&n)
		} else {
			err = tx.QueryRowContext(ctx, `SELECT count(*) FROM login_failures WHERE ip = ? AND account = ? AND at > ?`, ip, l.Account, since).Scan(&n)
		}
		if err != nil {
			return err
		}
		if n >= l.Max {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO login_blocks (key, until) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET until = max(until, excluded.until)`,
				l.Key, now.Add(l.Block).Unix())
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ClearFailures forgets the failed sign-ins of account from ip, after a successful one.
func (s *Store) ClearFailures(ctx context.Context, account, ip string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_failures WHERE account = ? AND ip = ?`, account, ip)
	return err
}
