-- A passkey that signs an account in. The account password is sealed under a key derived from
-- the passkey's PRF output, which only the passkey can produce, or (009) from
-- POSTBOX_PASSKEY_KEY; nothing in this database opens it.
CREATE TABLE passkeys (
    id          BLOB PRIMARY KEY,  -- the credential id
    account     TEXT NOT NULL,     -- lower-case email address
    user_handle BLOB NOT NULL,     -- the account's WebAuthn user handle, shared by its passkeys
    credential  TEXT NOT NULL,     -- the verified credential (public key, counters, flags) as JSON
    sealed      BLOB NOT NULL,     -- AES-256-GCM nonce and ciphertext of the password
    created_at  INTEGER NOT NULL   -- unix seconds
) STRICT;
CREATE INDEX passkeys_account ON passkeys (account);
