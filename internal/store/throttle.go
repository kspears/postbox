package store

import (
	"context"
	"time"
)

// Blocks are keyed "<account> <ip>" for one address from one IP, and "<ip>" for a whole IP.
func pairKey(account, ip string) string { return account + " " + ip }

// Blocked reports whether account may not sign in from ip at now, because of a block on that
// pair or on the whole IP.
func (s *Store) Blocked(ctx context.Context, now time.Time, account, ip string) (bool, error) {
	var blocked bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM login_blocks WHERE key IN (?, ?) AND until > ?)`,
		pairKey(account, ip), ip, now.Unix(),
	).Scan(&blocked)
	return blocked, err
}

// Limits are the throttle rules: PerAccountIP failures for one address from one IP, or PerIP
// failures from one IP across addresses, within Window block that pair or IP for Block.
type Limits struct {
	PerAccountIP, PerIP int
	Window, Block       time.Duration
}

// RecordFailure stores a failed sign-in for account from ip, and adds a block for each limit
// it reaches. It also forgets failures and blocks that no longer matter.
func (s *Store) RecordFailure(ctx context.Context, now time.Time, account, ip string, l Limits) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	since := now.Add(-l.Window).Unix()
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_failures WHERE at <= ?`, since); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_blocks WHERE until <= ?`, now.Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO login_failures (account, ip, at) VALUES (?, ?, ?)`, account, ip, now.Unix()); err != nil {
		return err
	}
	var forPair, forIP int
	err = tx.QueryRowContext(ctx,
		`SELECT count(*) FILTER (WHERE account = ?), count(*) FROM login_failures WHERE ip = ?`,
		account, ip,
	).Scan(&forPair, &forIP)
	if err != nil {
		return err
	}
	until := now.Add(l.Block).Unix()
	for key, reached := range map[string]bool{pairKey(account, ip): forPair >= l.PerAccountIP, ip: forIP >= l.PerIP} {
		if !reached {
			continue
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO login_blocks (key, until) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET until = max(until, excluded.until)`,
			key, until)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearFailures forgets the failed sign-ins of account from ip, after a successful one.
func (s *Store) ClearFailures(ctx context.Context, account, ip string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_failures WHERE account = ? AND ip = ?`, account, ip)
	return err
}
