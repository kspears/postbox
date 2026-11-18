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
MINIMAL=postbox-ci-minimal

cleanup() {
	status=$?
	if [ "$status" -ne 0 ] || [ -n "${CI_SHOW_LOGS:-}" ]; then
		echo "--- container logs ---" >&2
		docker compose --profile tools logs --no-color --tail 200 >&2 || true
	fi
	docker rm --force "$MINIMAL" >/dev/null 2>&1 || true
	docker compose --profile tools down --volumes --remove-orphans >/dev/null 2>&1 || true
	exit "$status"
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

step "build images"
docker compose --profile tools pull --ignore-buildable --quiet &
docker compose --profile tools build --quiet
wait

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
docker run --detach --name "$MINIMAL" --network postbox-ci_default \
	-e POSTBOX_IMAP_ADDR=dovecot:993 -e POSTBOX_SMTP_ADDR=postfix:465 \
	-e POSTBOX_PUBLIC_URL=https://mail.example.test postbox:ci serve >/dev/null
# The browser image is the one container here with an HTTP client.
docker compose run --rm -T --no-deps --entrypoint node e2e --input-type=module --eval "
	for (let i = 0; i < 40; i++) {
		try {
			const r = await fetch('http://$MINIMAL:8080/healthz');
			if (r.status === 200 && (await r.text()) === 'ok') process.exit(0);
		} catch {}
		await new Promise((resolve) => setTimeout(resolve, 250));
	}
	process.exit(1);
" || fail "/healthz did not answer 200 ok on the default port"

step "browser tests"
docker compose run --rm -T e2e

printf '\nall checks passed\n'
