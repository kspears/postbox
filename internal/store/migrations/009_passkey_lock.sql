-- What a passkey's password is sealed under: a key from its PRF output ('prf'), or, for a
-- passkey without PRF, a key from POSTBOX_PASSKEY_KEY ('server').
ALTER TABLE passkeys ADD COLUMN lock TEXT NOT NULL DEFAULT 'prf' CHECK (lock IN ('prf', 'server'));
