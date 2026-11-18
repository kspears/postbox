#!/usr/bin/env bash
# Fills the test mailboxes, once:
#   seed/<account>/*.eml      delivered to <account>@example.test through postfix
#   seed/<account>/Sent/*.eml written straight into that account's Sent folder
#   testdata/hostile/*.eml    written straight into hostile@example.test's Inbox
# A folder that already has mail is left alone, so `just dev` can run again without piling up
# copies. Finds the stack the way `docker compose` does: COMPOSE_PROJECT_NAME and COMPOSE_FILE,
# or this directory's compose.yaml.
set -euo pipefail

cd "$(dirname "$0")"
# Prints the number of messages in a folder, or nothing while dovecot is not ready. doveadm
# prints a header line, then "<folder><tab><count>".
count() { docker compose exec -T dovecot doveadm -f tab mailbox status -u "$1" messages "$2" 2>/dev/null | awk 'NR == 2 { print $2 }'; }

# Waits until folder holds want messages.
wait_for() {
	for _ in $(seq 50); do
		[ "$(count "$1" "$2")" = "$3" ] && return 0
		sleep 0.2
	done
	echo "seed: $1 $2 did not reach $3 messages" >&2
	exit 1
}

# dovecot answers once its auth and storage are up; until then postfix would defer deliveries
# for minutes.
for _ in $(seq 100); do
	[ -n "$(count alice@example.test INBOX)" ] && break
	sleep 0.2
done
[ -n "$(count alice@example.test INBOX)" ] || { echo "seed: dovecot did not become ready" >&2; exit 1; }

# Writes the given files into account's folder, in order, unless it has mail already.
save() {
	local account=$1 folder=$2 have want=0
	shift 2
	have=$(count "$account" "$folder")
	[ -n "$have" ] || { echo "seed: cannot read $folder of $account" >&2; exit 1; }
	[ "$have" = 0 ] || { echo "seed: $account $folder already has $have messages, left alone"; return 0; }
	for message in "$@"; do
		docker compose exec -T dovecot doveadm save -u "$account" -m "$folder" <"$message"
		want=$((want + 1))
	done
	echo "seed: $account $folder has $want messages"
}

for dir in seed/*/; do
	account="$(basename "$dir")@example.test"
	have=$(count "$account" INBOX)
	[ -n "$have" ] || { echo "seed: cannot read the Inbox of $account" >&2; exit 1; }
	if [ "$have" != 0 ]; then
		echo "seed: $account already has $have messages, left alone"
		continue
	fi
	want=0
	for message in "$dir"*.eml; do
		# Envelope sender is outside the stack, as for any mail arriving from elsewhere.
		docker compose exec -T postfix sendmail -f seed@example.org "$account" <"$message"
		want=$((want + 1))
		# One at a time, so arrival order (and so UID order) follows the file names.
		wait_for "$account" INBOX "$want"
	done
	echo "seed: $account has $want messages"
	if [ -d "${dir}Sent" ]; then
		save "$account" Sent "${dir}"Sent/*.eml
	fi
done

save hostile@example.test INBOX ../../testdata/hostile/*.eml
