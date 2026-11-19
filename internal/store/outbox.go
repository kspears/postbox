package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Send is a row of pending_sends.
type Send struct {
	ID           string `json:"id"`
	Account      string `json:"-"`
	DraftsFolder string `json:"draftsFolder"`
	DraftUID     uint32 `json:"draftUid"`
	State        string `json:"state"`
	Error        string `json:"error,omitempty"`
	CreatedAt    time.Time
}

// AddSend records a message entering the outbox.
func (s *Store) AddSend(ctx context.Context, send Send) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_sends (id, account, drafts_folder, draft_uid, state, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		send.ID, send.Account, send.DraftsFolder, send.DraftUID, send.State, send.Error, send.CreatedAt.Unix())
	return err
}

// SetSendState records what became of a message.
func (s *Store) SetSendState(ctx context.Context, id, state, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_sends SET state = ?, error = ? WHERE id = ?`, state, errText, id)
	return err
}

// GetSend returns one of account's sends, or ErrNotFound.
func (s *Store) GetSend(ctx context.Context, account, id string) (Send, error) {
	send := Send{ID: id, Account: account}
	var created int64
	err := s.db.QueryRowContext(ctx,
		`SELECT drafts_folder, draft_uid, state, error, created_at FROM pending_sends WHERE account = ? AND id = ?`, account, id,
	).Scan(&send.DraftsFolder, &send.DraftUID, &send.State, &send.Error, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Send{}, ErrNotFound
	}
	send.CreatedAt = time.Unix(created, 0)
	return send, err
}

// DeleteSend forgets a send.
func (s *Store) DeleteSend(ctx context.Context, account, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_sends WHERE account = ? AND id = ?`, account, id)
	return err
}

// SendsIn returns account's sends in the given states, oldest first.
func (s *Store) SendsIn(ctx context.Context, account string, states ...string) ([]Send, error) {
	var sends []Send
	for _, state := range states {
		rows, err := s.db.QueryContext(ctx,
			`SELECT id, drafts_folder, draft_uid, error, created_at FROM pending_sends WHERE account = ? AND state = ? ORDER BY created_at`, account, state)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			send := Send{Account: account, State: state}
			var created int64
			if err := rows.Scan(&send.ID, &send.DraftsFolder, &send.DraftUID, &send.Error, &created); err != nil {
				rows.Close()
				return nil, err
			}
			send.CreatedAt = time.Unix(created, 0)
			sends = append(sends, send)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return sends, nil
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
