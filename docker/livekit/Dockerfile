# LiveKit Server for Render
FROM alpine:3.19

# Download LiveKit binary
RUN apk add --no-cache curl && \
    curl -L https://github.com/livekit/livekit/releases/download/v1.9.11/livekit-server_1.9.11_linux_amd64.tar.gz | tar xz && \
    mv livekit-server /usr/local/bin/ && \
    chmod +x /usr/local/bin/livekit-server && \
    apk del curl

# Copy config
COPY livekit-render.yaml /livekit.yaml

# Expose port (Render handles HTTPS)
EXPOSE 7880

# Environment variables for config
ENV LIVEKIT_API_KEY=devkey
ENV LIVEKIT_API_SECRET=""

# Start LiveKit
CMD ["livekit-server", "--config", "/livekit.yaml"]