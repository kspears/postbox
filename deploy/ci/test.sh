#!/usr/bin/env bash
# Integration test: brings up the test stack and drives Postbox the way a browser would.
# Needs docker with compose v2 and openssl on the host. Run from anywhere:
#   deploy/ci/test.sh
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROGRESS=quiet
# This script tears its stack down with its volumes at the end. Pin which stack that is, so a
# COMPOSE_PROJECT_NAME or COMPOSE_FILE left in the caller's environment (say, the dev stack's)
# can never point it at another one.
export COMPOSE_PROJECT_NAME=postbox-ci
unset COMPOSE_FILE
# Both seeded accounts share it. Hex only, so it needs no quoting anywhere.
SEED_PASSWORD="ci-$(openssl rand -hex 12)"
export SEED_PASSWORD

cleanup() {
	status=$?
	if [ "$status" -ne 0 ] || [ -n "${CI_SHOW_LOGS:-}" ]; then
		echo "--- container logs ---" >&2
		docker compose --profile tools logs --no-color --tail 200 >&2 || true
	fi
	docker rm --force postbox-ci-outage >/dev/null 2>&1 || true
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
# 60 one-message conversations for the paging test: more than one page of 50. Written straight
# into the mailbox; the dovecot image has a shell with printf and nothing else.
docker compose exec -T dovecot sh -c '
	i=1
	while [ $i -le 60 ]; do
		printf "From: Sender %d <sender%d@example.org>\r\nTo: paging@example.test\r\nSubject: Paging %02d\r\nMessage-ID: <paging-%d@example.org>\r\nDate: Mon, 2 Nov 2026 %02d:%02d:00 +0000\r\n\r\nMessage %d.\r\n" $i $i $i $i $((i / 60)) $((i % 60)) $i |
			doveadm save -u paging@example.test -m INBOX || exit 1
		i=$((i + 1))
	done'

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
docker compose run --rm -T -e POSTBOX_URL=http://postbox-minimal:8080 e2e --project main --grep "health check"

step "browser tests"
docker compose run --rm -T e2e --project main --project restart-before

step "criterion 2: the blocked sign-in never reached dovecot"
# The throttling test failed five times from 203.0.113.20, then was blocked. Postbox passes the
# address on with ID, so dovecot logs it as rip=. A sixth line would mean the block leaked.
logins=$(docker compose logs --no-color dovecot | grep -c 'rip=203\.0\.113\.20,' || true)
[ "$logins" = 5 ] || fail "dovecot saw $logins logins from 203.0.113.20, want 5"
echo "dovecot saw 5 logins from 203.0.113.20"

step "criteria 12 and 13: nothing reached the sink but the images the reader allowed"
# The only request the sink may have seen is the remote photo of "Pictures 1" and "Pictures 2",
# loaded after "Always load from this sender". Anything else got past the sanitiser and the
# frame's policy.
unexpected=$(docker compose logs --no-color sink | grep -E '"(GET|POST|HEAD) ' | grep -vE '"GET /remote-[12]\.png ' || true)
[ -z "$unexpected" ] || fail "the sink was asked for something it should not have been: $unexpected"
echo "sink: no unexpected requests"

step "criterion 28: the banner shows while dovecot is down and clears when it is back"
docker compose run --detach --name postbox-ci-outage -T e2e --project outage >/dev/null
state() { docker exec postbox-ci-outage test -f "/e2e/.state/$1"; }
wait_state() {
	for _ in $(seq 240); do
		state "$1" && return 0
		docker inspect -f '{{.State.Running}}' postbox-ci-outage | grep -q true || break
		sleep 0.25
	done
	docker logs postbox-ci-outage >&2 || true
	fail "the outage test did not reach $1"
}
wait_state outage-ready
docker compose stop dovecot
wait_state outage-banner
docker compose start dovecot
docker exec postbox-ci-outage touch /e2e/.state/outage-restarted
status=$(docker wait postbox-ci-outage)
docker logs postbox-ci-outage 2>&1 | tail -5
docker rm postbox-ci-outage >/dev/null
[ "$status" = 0 ] || fail "the outage test failed"

step "criterion 28: an untrusted mail-server certificate stops sign-in"
docker compose up --detach postbox-untrusted
docker compose run --rm -T -e POSTBOX_URL=http://postbox-untrusted.localhost e2e --project untrusted
docker compose logs --no-color postbox-untrusted | grep -q 'certificate' || fail "postbox-untrusted did not log the certificate error"

step "criterion 20: with no special-use folders, the Sent copy goes to \"Sent Items\""
docker compose up --detach dovecot-plain postbox-plain
for _ in $(seq 50); do
	docker compose exec -T dovecot-plain doveadm mailbox create -u writer@example.test "Sent Items" 2>/dev/null && break
	sleep 0.2
done
docker compose run --rm -T -e POSTBOX_URL=http://postbox-plain.localhost e2e --project plain

step "criterion 7: sessions survive a restart; a changed password signs the session out"
# The accounts file lives in a volume; the init image has sed.
docker compose run --rm --no-deps -T --entrypoint sh init -c \
	'sed -i "s/^changer@example.test:{PLAIN}.*/changer@example.test:{PLAIN}changed-$(date +%s)::::::/" /auth/users'
# Criterion 24, restart half: a message sent just before the restart is still in its undo
# window when Postbox stops, so it must stay in Drafts and never be delivered.
docker compose run --rm -T e2e --project send-before-restart
docker compose restart postbox
docker compose run --rm -T e2e --project restart-after

step "criterion 7: session lifetimes (moves the clock, so last)"
docker compose run --rm -T e2e --project clock

printf '\nall checks passed\n'
