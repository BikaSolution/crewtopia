# Base images are pinned by tag AND digest so a re-pushed tag can't silently
# change what we build on. Dependabot (.github/dependabot.yml) bumps both.
FROM golang:1.27.3-alpine3.24@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS builder
WORKDIR /src

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS=-mod=readonly \
    GOTOOLCHAIN=local

COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy only what the build needs (never `COPY . .`), so nothing stray in the
# build context — .env files, databases, secrets — can end up in a layer.
COPY cmd ./cmd
COPY internal ./internal
RUN go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /out/server ./cmd/server

# Empty dir for the SQLite volume, owned by the runtime user. A *new* named
# volume mounted at /data inherits this ownership.
RUN mkdir -p /out/data

# Pure Go build (modernc.org/sqlite, no CGO) means the final stage needs no
# libc at all — distroless/static is enough: no shell, no package manager.
# :nonroot runs as uid/gid 65532. web/templates and web/static are read from
# disk at runtime (not embedded), so they're copied in root-owned and
# therefore read-only for the app user.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
WORKDIR /app

COPY --from=builder /out/server ./server
COPY web ./web
COPY --from=builder --chown=65532:65532 /out/data /data

USER 65532:65532
ENV CREWTOPIA_ADDR=:8080
EXPOSE 8080

ENTRYPOINT ["/app/server"]
