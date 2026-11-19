-- Addresses the person has sent mail to, for autocomplete in the recipient fields. One row per
-- account and address; email is lower-cased, name is the newest display name seen (or empty),
-- used_at is the unix time of the latest message and uses counts the messages.
CREATE TABLE collected_addresses (
    account TEXT NOT NULL,
    email   TEXT NOT NULL,
    name    TEXT NOT NULL DEFAULT '',
    used_at INTEGER NOT NULL,
    uses    INTEGER NOT NULL,
    PRIMARY KEY (account, email)
) STRICT;
