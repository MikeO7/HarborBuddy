# --- Build stage --------------------------------------------------
FROM golang:1.25-alpine AS builder

# Install timezone data for timezone support
RUN apk add --no-cache ca-certificates tzdata

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

# Copy CA certificates for HTTPS connections
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy timezone data for timezone support
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

COPY --from=builder /harborbuddy /harborbuddy

ENV HARBORBUDDY_CONFIG=/config/harborbuddy.yml

ENTRYPOINT ["/harborbuddy"]

