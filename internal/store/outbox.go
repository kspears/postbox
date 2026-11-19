package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Send is a row of pending_sends.
type Send struct {
	ID           string `json:"id"`
	Account      string `json:"-"`
	DraftsFolder string `json:"draftsFolder"`
	DraftUID     uint32 `json:"draftUid"`
	State        string `json:"state"`
	// Reason says why a send failed (see migrations/005_send_reason.sql); Error holds the
	// server's own words when it refused the message.
	Reason    string `json:"reason,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt time.Time
}

// AddSend records a message entering the outbox.
func (s *Store) AddSend(ctx context.Context, send Send) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_sends (id, account, drafts_folder, draft_uid, state, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		send.ID, send.Account, send.DraftsFolder, send.DraftUID, send.State, send.Error, send.CreatedAt.Unix())
	return err
}

// SetSendState records what became of a message: its state, and for a failure the reason and
// the server's words.
func (s *Store) SetSendState(ctx context.Context, id, state, reason, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_sends SET state = ?, reason = ?, error = ? WHERE id = ?`, state, reason, errText, id)
	return err
}

// GetSend returns one of account's sends, or ErrNotFound.
func (s *Store) GetSend(ctx context.Context, account, id string) (Send, error) {
	send := Send{ID: id, Account: account}
	var created int64
	err := s.db.QueryRowContext(ctx,
		`SELECT drafts_folder, draft_uid, state, reason, error, created_at FROM pending_sends WHERE account = ? AND id = ?`, account, id,
	).Scan(&send.DraftsFolder, &send.DraftUID, &send.State, &send.Reason, &send.Error, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Send{}, ErrNotFound
	}
	send.CreatedAt = time.Unix(created, 0)
	return send, err
}

// SendWaiting reports whether the draft at folder/uid is in its undo window or being sent.
func (s *Store) SendWaiting(ctx context.Context, account, folder string, uid uint32) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM pending_sends WHERE account = ? AND drafts_folder = ? AND draft_uid = ? AND state IN ('waiting', 'submitting')`,
		account, folder, uid).Scan(&n)
	return n > 0, err
}

// DeleteSentBefore forgets account's sends that went out before t; nobody is still waiting to
// hear about them.
func (s *Store) DeleteSentBefore(ctx context.Context, account string, t time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_sends WHERE account = ? AND state = 'sent' AND created_at < ?`, account, t.Unix())
	return err
}

// DeleteSend forgets a send.
func (s *Store) DeleteSend(ctx context.Context, account, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_sends WHERE account = ? AND id = ?`, account, id)
	return err
}

// SendsIn returns account's sends in the given states, oldest first.
func (s *Store) SendsIn(ctx context.Context, account string, states ...string) ([]Send, error) {
	if len(states) == 0 {
		return nil, nil
	}
	args := []any{account}
	for _, state := range states {
		args = append(args, state)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, drafts_folder, draft_uid, state, reason, error, created_at FROM pending_sends
		 WHERE account = ? AND state IN (?`+strings.Repeat(",?", len(states)-1)+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sends []Send
	for rows.Next() {
		send := Send{Account: account}
		var created int64
		if err := rows.Scan(&send.ID, &send.DraftsFolder, &send.DraftUID, &send.State, &send.Reason, &send.Error, &created); err != nil {
			return nil, err
		}
		send.CreatedAt = time.Unix(created, 0)
		sends = append(sends, send)
	}
	return sends, rows.Err()
}

// SettleInterruptedSends runs at start-up. A message still in its undo window was never sent;
// one being handed over may or may not have been.
func (s *Store) SettleInterruptedSends(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE pending_sends SET state = 'unsent' WHERE state = 'waiting'`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE pending_sends SET state = 'unknown' WHERE state = 'submitting'`)
	return err
}
