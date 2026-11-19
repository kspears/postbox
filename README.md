# Postbox

A web mail client in one container. Postbox talks IMAP and SMTP submission to the mail server
you already run and keeps no copy of your mail.

Status: early development. You can sign in, browse folders and read mail, with remote images blocked and new mail arriving live. Organising and writing mail come next.
The specification is [docs/specs/postbox.md](docs/specs/postbox.md).

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

Postbox speaks plain HTTP; put a reverse proxy that terminates TLS in front of it. When
`POSTBOX_CLIENT_IP_HEADER` is set, the header is trusted from every request, so the listen port
must be reachable only through that proxy.

`GET /healthz` returns `200` with the body `ok` while the process is up.

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
HTML corpus from `testdata/hostile`. `just dev-down` stops the stack and keeps its mail.

## Licence

AGPL-3.0. See [LICENSE](LICENSE).
