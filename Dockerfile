# syntax=docker/dockerfile:1.27.0@sha256:bde3983e9c939224420ddaf6b784cc30e09b035a4dea01f581230c50809f372e

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    export CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH"; \
    if [ "$TARGETARCH" = "arm" ] && [ -n "$TARGETVARIANT" ]; then \
        export GOARM="${TARGETVARIANT#v}"; \
    fi; \
    go build \
        -trimpath \
        -buildvcs=false \
        -ldflags="-s -w \
          -X github.com/MikeO7/HarborBuddy/internal/buildinfo.Version=${VERSION} \
          -X github.com/MikeO7/HarborBuddy/internal/buildinfo.Commit=${COMMIT} \
          -X github.com/MikeO7/HarborBuddy/internal/buildinfo.Date=${DATE}" \
        -o /out/harborbuddy \
        ./cmd/harborbuddy

# scratch has no /tmp; the daemon writes its healthcheck status file there.
RUN mkdir -p /out/tmp && chmod 1777 /out/tmp

FROM scratch

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

LABEL org.opencontainers.image.title="HarborBuddy" \
      org.opencontainers.image.description="Transactional Docker container updater" \
      org.opencontainers.image.source="https://github.com/MikeO7/HarborBuddy" \
      org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.created="$DATE" \
      com.harborbuddy.role="daemon"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /out/harborbuddy /harborbuddy
COPY --from=builder /out/tmp /tmp

# The daemon pings Docker every 15s and records it; this fails when that
# heartbeat is older than 60s or an update cycle has run for over 2h.
HEALTHCHECK --interval=30s --timeout=10s --start-period=1m --start-interval=2s --retries=3 \
    CMD ["/harborbuddy", "--healthcheck"]

ENTRYPOINT ["/harborbuddy"]
