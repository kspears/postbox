-- A signed-in browser. The account password is sealed with a key that only the browser's cookie
-- holds, so this table alone cannot reveal it.
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,  -- random, also in the cookie
    account    TEXT NOT NULL,     -- lower-case email address
    sealed     BLOB NOT NULL,     -- AES-256-GCM nonce and ciphertext of the password
    method     TEXT NOT NULL CHECK (method IN ('password', 'passkey')),
    created_at INTEGER NOT NULL,  -- unix seconds
    expires_at INTEGER NOT NULL
) STRICT;
CREATE INDEX sessions_account ON sessions (account);
CREATE INDEX sessions_expires_at ON sessions (expires_at);

-- One row per failed sign-in, kept for the throttle window only.
CREATE TABLE login_failures (
    account TEXT NOT NULL,
    ip      TEXT NOT NULL,
    at      INTEGER NOT NULL
) STRICT;
CREATE INDEX login_failures_ip_at ON login_failures (ip, at);

-- A throttle block in force. key is "<account> <ip>" for one address, or "<ip>" for a whole IP.
CREATE TABLE login_blocks (
    key   TEXT PRIMARY KEY,
    until INTEGER NOT NULL
) STRICT;
