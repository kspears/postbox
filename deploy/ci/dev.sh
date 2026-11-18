#!/usr/bin/env bash
# Starts the test stack with Postbox on http://localhost:8026 and seeds two mailboxes. Safe to
# run again: it rebuilds Postbox and keeps the mail. Set POSTBOX_DEV_PORT when 8026 is taken.
#
# With arguments it runs `docker compose` on that stack instead: `dev.sh down`, `dev.sh logs`.
# Mind `down --volumes`: it deletes the mailboxes, including anything `just bench` generated.
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROJECT_NAME=postbox-dev COMPOSE_FILE=compose.yaml:compose.dev.yaml
export POSTBOX_DEV_PORT=${POSTBOX_DEV_PORT:-8026}

# The seeded accounts' password is generated once and kept out of git (.dev/ is ignored).
mkdir -p .dev
[ -s .dev/password ] || (umask 077 && printf 'dev-%s\n' "$(openssl rand -hex 8)" >.dev/password)
SEED_PASSWORD=$(<.dev/password)
export SEED_PASSWORD

if [ $# -gt 0 ]; then
	exec docker compose "$@"
fi

docker compose up --detach --build
./seed.sh

cat <<MESSAGE

Postbox is at http://localhost:$POSTBOX_DEV_PORT
Accounts: alice@example.test and bob@example.test
Password: in deploy/ci/.dev/password
MESSAGE
