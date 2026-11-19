-- Until when (unix seconds) the session may change passkeys and "Require passkey" without
-- entering the password again. Set at sign-in and by each confirmation.
ALTER TABLE sessions ADD COLUMN confirmed_until INTEGER NOT NULL DEFAULT 0;
