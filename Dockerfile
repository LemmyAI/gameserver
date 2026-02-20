# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy all source code
COPY . .

# Verify protobuf files exist
RUN ls -la internal/protocol/gamepb/

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux go build -v -o /gameserver ./cmd/server

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /gameserver /app/gameserver

# Render sets PORT env var - HTTP server uses this
# UDP is not supported on Render free tier
ENV PORT=10000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT}/health || exit 1

# Run the server
ENTRYPOINT ["/app/gameserver"]