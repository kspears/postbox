# Postbox: one static binary with the frontend embedded. Build context is the repository root.
# Both build stages run on the builder's own platform; Go cross-compiles, so a multi-platform
# build never runs a toolchain under emulation.
FROM --platform=$BUILDPLATFORM node:26-trixie-slim@sha256:e6bedef217677b276c591795e169840edd9096dbf13f29df8ef8d1856b8820bc AS web
WORKDIR /src/web
# Keep in step with "packageManager" in web/package.json.
RUN npm install --global pnpm@10.28.0
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie@sha256:af1bfc2614ebb1dbd31af514b78740fd678850e76498762b425ecee4c5c6ea80 AS build
ARG TARGETOS TARGETARCH
# Empty for a release. The test stack sets "citest", which adds endpoints only tests may have.
ARG GO_TAGS=""
WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
# CGO off: the result needs no libc, which is what lets the final image be distroless.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -tags="$GO_TAGS" -o /out/postbox ./cmd/postbox
# The final image has no shell to create the default state directory with.
RUN mkdir -p /out/data/postbox

# Distroless publishes no version tags, so the digest is the pin. Runs as uid 65532.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /out/postbox /usr/local/bin/postbox
# Owned by the runtime user, so a new named volume mounted here starts out writable.
COPY --from=build --chown=65532:65532 /out/data /data
EXPOSE 8080
ENTRYPOINT ["postbox"]
CMD ["serve"]
