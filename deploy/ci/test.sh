#!/usr/bin/env bash
# Integration test: brings up the test stack and drives Postbox the way a browser would.
# Needs docker with compose v2 and openssl on the host. Run from anywhere:
#   deploy/ci/test.sh
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROGRESS=quiet
# Both seeded accounts share it. Hex only, so it needs no quoting anywhere.
SEED_PASSWORD="ci-$(openssl rand -hex 12)"
export SEED_PASSWORD

cleanup() {
	status=$?
	if [ "$status" -ne 0 ] || [ -n "${CI_SHOW_LOGS:-}" ]; then
		echo "--- container logs ---" >&2
		docker compose --profile tools logs --no-color --tail 200 >&2 || true
	fi
	docker compose --profile tools down --volumes --remove-orphans >/dev/null 2>&1 || true
	exit "$status"
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

step "build images"
docker compose --profile tools pull --ignore-buildable --quiet &
pull=$!
docker compose --profile tools build --quiet
wait "$pull" || fail "could not pull the pinned images"

step "start the stack and seed the mailboxes"
docker compose up --detach
./seed.sh

step "the seeded accounts can sign in to dovecot, and a wrong password cannot"
docker compose exec -T -e SEED_PASSWORD dovecot sh -c 'doveadm auth test alice@example.test "$SEED_PASSWORD"' >/dev/null ||
	fail "alice could not authenticate"
if docker compose exec -T dovecot doveadm auth test alice@example.test wrong-password >/dev/null 2>&1; then
	fail "a wrong password was accepted"
fi

step "postbox refuses to start without POSTBOX_IMAP_ADDR"
out=$(docker run --rm -e POSTBOX_SMTP_ADDR=postfix:465 -e POSTBOX_PUBLIC_URL=https://mail.example.test postbox:ci serve 2>&1) &&
	fail "postbox started without POSTBOX_IMAP_ADDR"
[ "$out" = "error: POSTBOX_IMAP_ADDR is required (host:port)" ] || fail "unexpected start-up error: $out"

step "postbox starts with only the three required settings, and /healthz answers ok"
docker compose up --detach postbox-minimal
# The browser image is the one container here with an HTTP client; its health test waits for
# Postbox to listen, then expects 200 and "ok" on the default port.
docker compose run --rm -T -e POSTBOX_URL=http://postbox-minimal:8080 e2e --grep "health check"

step "browser tests"
docker compose run --rm -T e2e

printf '\nall checks passed\n'
