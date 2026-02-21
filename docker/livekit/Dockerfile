# LiveKit Server for Render
FROM alpine:3.19

WORKDIR /app

# Install dependencies
RUN apk add --no-cache curl wget && \
    curl -L https://github.com/livekit/livekit/releases/download/v1.9.11/livekit-server_1.9.11_linux_amd64.tar.gz | tar xz -C /tmp && \
    mv /tmp/livekit-server /usr/local/bin/ && \
    chmod +x /usr/local/bin/livekit-server && \
    apk del curl

# Expose port (Render handles HTTPS termination)
EXPOSE 7880

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:7880 || exit 1

# Environment variables (set via Render dashboard)
ENV LIVEKIT_API_KEY=devkey
ENV LIVEKIT_API_SECRET=""
ENV PORT=7880

# Start LiveKit with minimal config
CMD ["sh", "-c", "livekit-server --dev --listen-address :${PORT:-7880}"]