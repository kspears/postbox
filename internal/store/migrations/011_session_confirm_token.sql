-- The SHA-256 of the confirmation token the browser tab holds: confirmed_until applies only to
-- requests that carry it, so a copy of the session cookie alone cannot change passkeys.
ALTER TABLE sessions ADD COLUMN confirm_hash BLOB;
