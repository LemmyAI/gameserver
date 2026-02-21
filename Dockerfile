# GameServer - Multiplayer game server with webbridge
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build both server and webbridge
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /webbridge ./cmd/webbridge

# Final image
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates

# Copy binaries
COPY --from=builder /server /app/bin/server
COPY --from=builder /webbridge /app/bin/webbridge

# Copy static files (webbridge needs these)
COPY cmd/webbridge/public /app/cmd/webbridge/public

# Render sets PORT env var - webbridge listens on this
# HTTPS is handled by Render's load balancer
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:8080/status || exit 1

# Start webbridge (it spawns game servers as needed)
CMD ["/app/bin/webbridge"]