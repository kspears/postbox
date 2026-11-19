-- Messages handed to Postbox's outbox. The message itself is a draft in the Drafts folder; this
-- row says what became of it, so the person can be told even after closing the tab or a
-- restart. state: waiting (in the undo window), submitting (being handed to the server),
-- sent, sent_no_copy (sent, but the copy to Sent failed), failed (refused, with the server's
-- words in error), unsent (Postbox stopped during the window), unknown (Postbox stopped while
-- handing it over).
CREATE TABLE pending_sends (
    id            TEXT PRIMARY KEY,
    account       TEXT NOT NULL,
    drafts_folder TEXT NOT NULL,
    draft_uid     INTEGER NOT NULL,
    state         TEXT NOT NULL,
    error         TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL
) STRICT;
CREATE INDEX pending_sends_account ON pending_sends (account);
