#!/usr/bin/env bash
# Delivers the messages in seed/<account>/ to <account>@example.test through postfix, once.
# An account that already has mail in its Inbox is left alone, so `just dev` can run again
# without piling up copies. Finds the stack the way `docker compose` does: COMPOSE_PROJECT_NAME
# and COMPOSE_FILE, or this directory's compose.yaml.
set -euo pipefail

cd "$(dirname "$0")"
# Prints the number of messages in the account's Inbox, or nothing while dovecot is not ready.
# doveadm prints a header line, then "INBOX<tab><count>".
count() { docker compose exec -T dovecot doveadm -f tab mailbox status -u "$1" messages INBOX 2>/dev/null | awk 'NR == 2 { print $2 }'; }

# dovecot answers once its auth and storage are up; until then postfix would defer deliveries
# for minutes.
for _ in $(seq 60); do
	[ -n "$(count alice@example.test)" ] && break
	sleep 1
done
[ -n "$(count alice@example.test)" ] || { echo "seed: dovecot did not become ready in 60s" >&2; exit 1; }

for dir in seed/*/; do
	account="$(basename "$dir")@example.test"
	[ "$(count "$account")" = 0 ] || continue
	want=0
	for message in "$dir"*.eml; do
		# Envelope sender is outside the stack, as for any mail arriving from elsewhere.
		docker compose exec -T postfix sendmail -f seed@example.org "$account" <"$message"
		want=$((want + 1))
		# One at a time, so arrival order (and so UID order) follows the file names.
		for _ in $(seq 30); do
			[ "$(count "$account")" = "$want" ] && break
			sleep 1
		done
		[ "$(count "$account")" = "$want" ] || { echo "seed: $message was not delivered to $account in 30s" >&2; exit 1; }
	done
	echo "seed: $account has $want messages"
done
