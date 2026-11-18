#!/bin/sh
# Runs once before dovecot and postfix start. Everything it writes goes into named volumes, so
# `docker compose down -v` removes all of it and nothing lands in the checkout.
set -eu
: "${SEED_PASSWORD:?the password of the seeded accounts; deploy/ci/test.sh and dev.sh set it}"

# Docker creates named volumes root-owned; mail belongs to uid 5000 (mail_uid in dovecot.conf).
chown 5000:5000 /var/vmail /var/vmail_index

if [ ! -f /keys/tls/tls.crt ]; then
	mkdir -p /keys/tls /keys/mail_crypt
	# One self-signed certificate for both daemons. It is its own authority, so the public
	# half doubles as the CA file Postbox is given.
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 3650 \
		-subj /CN=mail.example.test \
		-addext subjectAltName=DNS:dovecot,DNS:postfix,DNS:mail.example.test \
		-keyout /keys/tls/tls.key -out /keys/tls/tls.crt 2>/dev/null
	# dovecot requires PKCS#8 ("BEGIN PRIVATE KEY"); a bare `ecparam -genkey` emits "EC PRIVATE KEY".
	openssl ecparam -name prime256v1 -genkey -noout | openssl pkey -out /keys/mail_crypt/ecprivkey.pem
	openssl pkey -in /keys/mail_crypt/ecprivkey.pem -pubout -out /keys/mail_crypt/ecpubkey.pem
	# Throwaway keys, and the mail user must be able to read the private ones.
	chmod -R a+rX /keys
fi
cp /keys/tls/tls.crt /ca/ca.crt

# The accounts, rewritten at every start so they always match the caller's SEED_PASSWORD.
# {PLAIN} keeps the file writable by a test that changes a password: no hashing tool is needed,
# and dovecot does not cache lookups.
# bench is mailbench's account: it receives no mail, so postfix does not know it.
for user in alice bob bench; do
	printf '%s@example.test:{PLAIN}%s::::::\n' "$user" "$SEED_PASSWORD"
done >/auth/users
