# Postbox

A web mail client in one container. Postbox talks IMAP and SMTP submission to the mail server
you already run and keeps no copy of your mail.

Status: feature-complete for its first release, not yet released. Postbox signs in with the
mail password or a passkey, shows conversations with search and keyboard shortcuts, organises
mail across folders, and writes plain or rich messages with attachments, identities and an undo
window for sending. The specification is [docs/specs/postbox.md](docs/specs/postbox.md).

## Configuration

Postbox is configured with environment variables and exits at start-up with an error naming
the variable when a required one is missing or malformed.

| Variable | Required | Meaning |
| --- | --- | --- |
| `POSTBOX_IMAP_ADDR` | yes | IMAP server, `host:port`. |
| `POSTBOX_SMTP_ADDR` | yes | Submission server, `host:port`. |
| `POSTBOX_PUBLIC_URL` | yes | The `https://` URL users open, with no path. `http://` is accepted only for `localhost` and names under `.localhost`. |
| `POSTBOX_STATE` | no | State directory. Default `/data/postbox`. |
| `POSTBOX_LISTEN` | no | HTTP listen address. Default `:8080`. |
| `POSTBOX_CLIENT_IP_HEADER` | no | Request header that carries the real client IP, set by the reverse proxy. |
| `POSTBOX_CA_FILE` | no | PEM file of extra certificate authorities trusted for the mail server. |
| `POSTBOX_PASSKEY_KEY` | no | 32 random bytes in base64 (`openssl rand -base64 32`). Lets passkeys without the PRF extension (1Password, Bitwarden) sign in by encrypting their stored passwords with it. Keep it secret and apart from the state directory; changing it breaks those passkeys until they are added again. |

Postbox speaks plain HTTP; put a reverse proxy that terminates TLS in front of it. When
`POSTBOX_CLIENT_IP_HEADER` is set, the header is trusted from every request, so the listen port
must be reachable only through that proxy.

`GET /healthz` returns `200` with the body `ok` while the process is up.

## Deployment

The image is `ghcr.io/kspears/postbox`, for linux/amd64 and linux/arm64, published from each
version tag once the full check passes. GitHub creates the package private; make it public in
its package settings for others to pull it. The image runs as an unprivileged user.

```yaml
services:
  postbox:
    image: ghcr.io/kspears/postbox:1.0
    restart: unless-stopped
    environment:
      POSTBOX_IMAP_ADDR: mail.example.com:993
      POSTBOX_SMTP_ADDR: mail.example.com:465
      POSTBOX_PUBLIC_URL: https://webmail.example.com
      POSTBOX_CLIENT_IP_HEADER: X-Forwarded-For
      POSTBOX_PASSKEY_KEY: ${POSTBOX_PASSKEY_KEY}   # optional; see below
    volumes:
      - postbox:/data/postbox
    # Port 8080, on a network shared only with the reverse proxy: see below.
    networks: [proxy]

volumes:
  postbox:

networks:
  proxy:
```

**The reverse proxy** must serve Postbox at the root of `POSTBOX_PUBLIC_URL` over HTTPS and
not buffer responses on `/api/events`, which is a stream of server-sent events. Postbox reads
the client's address from the last entry of the header named by `POSTBOX_CLIENT_IP_HEADER`,
the one the proxy nearest Postbox adds; behind a CDN, have that proxy set the header to the real
address. Postbox trusts the header from anything that can reach it, so only the proxy may: a
container that reaches port 8080 directly can claim any address, past the sign-in throttle.

**The mail server** needs IMAP over implicit TLS (port 993, not STARTTLS on 143) and
submission over implicit TLS on 465 or STARTTLS on another port, with a certificate Postbox
trusts (add `POSTBOX_CA_FILE` for a private authority), and IMAP `THREAD` and `SORT`, which
Dovecot has.
Postbox works best when the mail server:

- marks the special folders (`\Archive`, `\Trash`, `\Drafts`, `\Sent`, `\Junk`); without
  them Postbox falls back to common folder names,
- indexes message bodies for full-text search, so body search is fast,
- trusts the Postbox container's address to forward the person's own address, so the mail
  server's limits and logs see people rather than Postbox: Dovecot's `login_trusted_networks`
  for the IMAP `ID` command's `x-originating-ip`, and Postfix's `smtpd_authorized_xclient_hosts`
  for `XCLIENT` on submission. Without this, every sign-in looks like it comes from Postbox,
  and the mail server's own throttling counts everyone together.

**State.** `/data/postbox/postbox.db` holds settings, identities, collected addresses, passkeys
and sessions, never mail. Losing it signs everyone out and forgets their settings and passkeys.
It is SQLite in WAL mode: back it up with Postbox stopped, or from an atomic snapshot of the
volume, copying `postbox.db` with its `-wal` file. A copy of it alone reveals no account
password.

**Passkeys** work for passkey providers with the WebAuthn PRF extension. For providers without
it, such as 1Password and Bitwarden, set `POSTBOX_PASSKEY_KEY` (see Configuration) and keep it
away from the state volume and its backups: the two together reveal the passwords stored for
those passkeys.

**A person who lost their passkey** while "Require passkey" was on is let back in by the
operator:

```sh
docker compose exec postbox postbox passkeys clear person@example.com
```

It prints `Cleared <n> passkeys for <address>. Require passkey is off.`, ends that account's
passkey sessions, and needs only `POSTBOX_STATE`.

## Development

Needs Go, Node, pnpm, [just](https://just.systems) and Docker.

```sh
just setup   # install dependencies
just dev     # Postbox on http://localhost:8026 with a test mail server and two mailboxes
just test    # Go unit tests
just check   # everything CI runs, including the browser tests in containers
```

`just dev` prints the seeded accounts; their password is generated on first run and kept in
`deploy/ci/.dev/password`. `reader@example.test` has a thread with a reply in Sent, messages
with remote and embedded images, and an attachment; `hostile@example.test` holds the hostile
HTML corpus from `testdata/hostile`; `organiser@example.test` has mail to archive, delete, move
and flag; `writer@example.test` is free to send from. `just dev-down` stops the stack and keeps its mail.

## Licence

AGPL-3.0. See [LICENSE](LICENSE).
