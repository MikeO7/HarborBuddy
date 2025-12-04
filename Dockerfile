# --- Build stage --------------------------------------------------
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Ensure go.mod and go.sum are properly synced
RUN go mod tidy

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -ldflags="-s -w" -o /harborbuddy ./cmd/harborbuddy

# --- Final image --------------------------------------------------
FROM scratch

COPY --from=builder /harborbuddy /harborbuddy

ENV HARBORBUDDY_CONFIG=/config/harborbuddy.yml

ENTRYPOINT ["/harborbuddy"]

