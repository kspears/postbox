#!/usr/bin/env bash
# Starts the test stack with Postbox on http://localhost:8026 and seeds two mailboxes. Safe to
# run again: it rebuilds Postbox and keeps the mail. Stop it with `just dev-down`. Set
# POSTBOX_DEV_PORT when 8026 is taken.
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROJECT_NAME=postbox-dev COMPOSE_FILE=compose.yaml:compose.dev.yaml
export POSTBOX_DEV_PORT=${POSTBOX_DEV_PORT:-8026}

# The seeded accounts' password is generated once and kept out of git (.dev/ is ignored).
mkdir -p .dev
[ -s .dev/password ] || (umask 077 && printf 'dev-%s\n' "$(openssl rand -hex 8)" >.dev/password)
SEED_PASSWORD=$(<.dev/password)
export SEED_PASSWORD

if [ "${1:-}" = down ]; then
	exec docker compose down
fi

docker compose up --detach --build
./seed.sh

cat <<MESSAGE

Postbox is at http://localhost:$POSTBOX_DEV_PORT
Accounts: alice@example.test and bob@example.test
Password: in deploy/ci/.dev/password
MESSAGE
