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

# Copy static files
COPY cmd/webbridge/public /app/cmd/webbridge/public

# Copy livekit config (for self-hosted option)
COPY livekit.yaml /app/livekit.yaml

# Expose ports
# 8080 - HTTP/WS (Render maps this to 443)
# 8443 - HTTPS (if using own cert)
# 7880 - LiveKit (if self-hosted)
# 9100-9200 - Game servers (UDP)
EXPOSE 8080 8443 7880

# Start webbridge (it spawns game servers as needed)
CMD ["/app/bin/webbridge"]