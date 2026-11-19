-- Why a send failed, as a code the browser turns into words: rejected (the server refused the
-- message; its words are in error), refused (the password), unreachable, unreadable (the draft
-- was gone) or invalid (no sender or recipients).
ALTER TABLE pending_sends ADD COLUMN reason TEXT NOT NULL DEFAULT '';
