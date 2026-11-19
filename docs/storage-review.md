# Storage review

Criterion 28's review step: a person reads the code below and confirms that no key able to
decrypt a stored password is written to the database or to disk, and that the only one read
from configuration is `POSTBOX_PASSKEY_KEY`, used only for passkeys without PRF. Record the
date, the commit and the reviewer at the end.

## Where an account password is kept

| Place | Sealed under | Where that key lives |
| --- | --- | --- |
| `sessions.sealed` | A random 32-byte key per session | Only in the browser's session cookie |
| `passkeys.sealed`, `lock = 'prf'` | HKDF of the passkey's PRF output and credential id | Only in the passkey; the browser sends the output at each sign-in |
| `passkeys.sealed`, `lock = 'server'` | HKDF of `POSTBOX_PASSKEY_KEY` and the credential id | The environment, never the database |
| Memory, during a send's undo window | Not sealed | Process memory only, gone at exit |
| Memory, a sign-in waiting for a new password | The passkey's derived key | Process memory, at most 2 minutes |

Every sealed value uses AES-256-GCM, and is bound to its row (session id or credential id) and
account through the associated data, so it cannot be moved to another row.

## What to read

1. `internal/secret/secret.go`: `Seal` and `Open`. The nonce is random and stored with the
   ciphertext; the key is never stored.
2. `internal/session/session.go`: `Create` makes the key, seals the password, stores only the
   sealed value, and puts the key in the cookie. `FromRequest` reads the key from the cookie
   alone.
3. `internal/auth/passkeys.go`: `Enrol` and `SignIn`, through `lockKey`. The PRF output and the
   derived key are used and dropped; `NewPasswordNeeded` keeps the key in the in-memory
   ceremony only.
4. `internal/store/migrations/*.sql` and `internal/store/*.go`: no column holds a key, a PRF
   output, or a password in the clear.
5. `internal/outbox/outbox.go`: the password for a pending send is held in memory, and the
   `pending_sends` table records state and the server's reply, not the password.
6. `internal/config/config.go`: `POSTBOX_PASSKEY_KEY` is the only secret read from the
   environment.
7. Logging: `grep -rn 'log\.' internal cmd` shows no password, key, cookie or PRF value.

The automated half of this check is in `deploy/ci/test.sh` (criterion 4): it searches a copy of
`postbox.db` for the seeded password as plain text, base64 and hex.

## Reviews

| Date | Commit | Reviewer | Result |
| --- | --- | --- | --- |
