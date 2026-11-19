-- The addresses a person sends as, each with its signature (sanitised HTML). The first is the
-- login address, made when the account's identities are first read; it cannot be removed.
CREATE TABLE identities (
    id        INTEGER PRIMARY KEY,
    account   TEXT NOT NULL,
    name      TEXT NOT NULL DEFAULT '',
    email     TEXT NOT NULL,
    signature TEXT NOT NULL DEFAULT '',
    position  INTEGER NOT NULL
) STRICT;
CREATE INDEX identities_account ON identities (account, position);
