# Simple Dockerfile for Render - debug version
FROM golang:1.23-alpine

WORKDIR /app

# Install git and ca-certificates
RUN apk add --no-cache git ca-certificates

# Copy everything
COPY . .

# Build webbridge
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/webbridge ./cmd/webbridge

# Render sets PORT env var
ENV PORT=8080

# Expose port
EXPOSE 8080

# Start webbridge
CMD ["/app/bin/webbridge"]