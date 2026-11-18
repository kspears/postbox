set positional-arguments

# gofmt does not read go.mod's ignore list, so it is pointed at the Go sources by hand.
go_sources := "cmd internal web/embed.go"

[private]
default:
    @just --list

# Install Go modules and the frontend's and browser tests' dependencies
setup:
    go mod download
    pnpm --dir web install --frozen-lockfile
    pnpm --dir e2e install --frozen-lockfile

# Run Postbox with a test mail server and two seeded mailboxes on http://localhost:8026
dev:
    deploy/ci/dev.sh

# Stop the stack started by `just dev`, keeping its mail
dev-down:
    deploy/ci/dev.sh down

# Measure the dev stack's mail server: `just bench check`, `generate -n 50000`, `run`
bench *args:
    deploy/ci/dev.sh run --rm go go run ./cmd/mailbench "$@"

# Serve the passkey PRF test page on http://localhost:8027 (Ctrl-C stops it)
passkey-test:
    python3 -m http.server 8027 --bind 127.0.0.1 --directory tools/passkey-prf

# Full CI gate: lint, unit tests, build, then the integration and browser tests
check: lint test build stack-test

# Run the Go unit tests
test *args:
    go test ./... "$@"

# Fail on unformatted Go, then vet, type-check and lint
lint:
    @test -z "$(gofmt -l {{ go_sources }})" || { gofmt -l {{ go_sources }}; echo "run: just fmt"; exit 1; }
    go vet ./...
    pnpm --dir web run check
    pnpm --dir web run lint
    pnpm --dir e2e run check

# Format all Go code
fmt:
    gofmt -w {{ go_sources }}

# Build the frontend, then postbox into bin/
build:
    pnpm --dir web run build
    CGO_ENABLED=0 go build -o bin/postbox ./cmd/postbox

# Run the integration and browser tests against real dovecot and postfix containers
stack-test:
    deploy/ci/test.sh
