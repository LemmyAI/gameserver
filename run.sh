#!/bin/bash
# Start the full game server stack with LiveKit

set -e

cd "$(dirname "$0")"

echo "🚀 Starting GameServer stack..."

# Check if Docker is available for LiveKit
if command -v docker &> /dev/null; then
    echo "🎥 Starting LiveKit server (Docker)..."

    # Stop any existing LiveKit container
    docker stop livekit-server 2>/dev/null || true
    docker rm livekit-server 2>/dev/null || true

    # Start LiveKit
    docker run -d \
        --name livekit-server \
        -v "$(pwd)/livekit.yaml:/livekit.yaml:ro" \
        -p 7880:7880 \
        -p 7881:7881 \
        -p 50000-50200:50000-50200/udp \
        livekit/livekit-server:latest \
        --config /livekit.yaml

    echo "✅ LiveKit started on ws://localhost:7880"
    sleep 2  # Give LiveKit time to start
else
    echo "⚠️  Docker not found - LiveKit must be running manually on ws://localhost:7880"
fi

# Set environment for webbridge
export LIVEKIT_URL="ws://localhost:7880"
export LIVEKIT_API_KEY="devkey"
export LIVEKIT_API_SECRET="secret123456789abcdefghij"

echo "🌐 Starting WebBridge on http://localhost:8081"
echo "📡 Game servers spawn per-room on ports 9100+"
echo ""
echo "🎮 Open http://localhost:8081 in your browser"
echo ""

# Run webbridge
./bin/webbridge