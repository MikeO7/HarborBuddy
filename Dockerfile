# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder

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

FROM scratch

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

LABEL org.opencontainers.image.title="HarborBuddy" \
      org.opencontainers.image.description="Transactional Docker container updater" \
      org.opencontainers.image.source="https://github.com/MikeO7/HarborBuddy" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.created="$DATE" \
      com.harborbuddy.role="daemon"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /out/harborbuddy /harborbuddy

ENTRYPOINT ["/harborbuddy"]
