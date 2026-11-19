#!/bin/sh
# Runs once before dovecot and postfix start. Everything it writes goes into named volumes, so
# `docker compose down -v` removes all of it and nothing lands in the checkout.
set -eu
: "${SEED_PASSWORD:?the password of the seeded accounts; deploy/ci/test.sh and dev.sh set it}"

# Docker creates named volumes root-owned; mail belongs to uid 5000 (mail_uid in dovecot.conf).
chown 5000:5000 /var/vmail /var/vmail_index /var/vmail_plain /var/vmail_plain_index

if [ ! -f /keys/tls/tls.crt ]; then
	mkdir -p /keys/tls /keys/mail_crypt
	# One self-signed certificate for both daemons. It is its own authority, so the public
	# half doubles as the CA file Postbox is given.
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 3650 \
		-subj /CN=mail.example.test \
		-addext subjectAltName=DNS:dovecot,DNS:dovecot-plain,DNS:postfix,DNS:mail.example.test \
		-keyout /keys/tls/tls.key -out /keys/tls/tls.crt 2>/dev/null
	# dovecot requires PKCS#8 ("BEGIN PRIVATE KEY"); a bare `ecparam -genkey` emits "EC PRIVATE KEY".
	openssl ecparam -name prime256v1 -genkey -noout | openssl pkey -out /keys/mail_crypt/ecprivkey.pem
	openssl pkey -in /keys/mail_crypt/ecprivkey.pem -pubout -out /keys/mail_crypt/ecpubkey.pem
	# Throwaway keys, and the mail user must be able to read the private ones.
	chmod -R a+rX /keys
fi
cp /keys/tls/tls.crt /ca/ca.crt

# The accounts. They are rewritten only when SEED_PASSWORD differs from the one they were
# written with, so a test that changes one password keeps its change when compose runs this
# again, and a new dev password still reaches every account. {PLAIN} keeps the file writable
# without a hashing tool; dovecot does not cache lookups. bench (mailbench), paging (a long list),
# changer (a password that changes mid-test) and hostile (the hostile HTML corpus) are filled
# directly by the tests and receive no mail, so postfix does not know them.
accounts="alice bob reader organiser writer bench paging changer hostile"
if [ ! -f /auth/users ] || [ "$(cat /auth/seed 2>/dev/null)" != "$SEED_PASSWORD" ]; then
	: >/auth/users
	printf '%s' "$SEED_PASSWORD" >/auth/seed
	chmod 600 /auth/seed
fi
# Accounts added since the file was written join it with the seed password.
for user in $accounts; do
	grep -q "^$user@example.test:" /auth/users ||
		printf '%s@example.test:{PLAIN}%s::::::\n' "$user" "$SEED_PASSWORD" >>/auth/users
done
