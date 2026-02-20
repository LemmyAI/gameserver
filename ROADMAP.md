# GameServer Build Plan

## Phase 1: Foundation ✅ COMPLETE

### Goal: UDP echo server that can send/receive messages

### Day 1-2: Project Setup
- [x] Initialize Go module
- [x] Create directory structure per SPEC.md
- [x] Set up Makefile for build/test
- [x] Add Dockerfile for development

### Day 3-5: Transport Layer
- [x] Define `Transport` interface in `internal/transport/transport.go`
- [x] Implement `UDPTransport` with basic send/receive
- [x] Implement `MockTransport` for testing
- [x] Write unit tests for transport

### Day 6-7: Message Protocol
- [x] Define protobuf messages in `proto/game.proto`
- [x] Generate Go code with protoc
- [x] Create `internal/protocol/` for encoding/decoding
- [x] Test serialization performance

### Day 8-10: Basic Server
- [x] Create `cmd/server/main.go`
- [ ] HTTP server with Fiber (health check endpoint) — Phase 3
- [x] UDP listener that echoes messages
- [x] Graceful shutdown handling

### Deliverable
```bash
✅ UDP echo working: echo "ping" | nc -u localhost 9000
```

---

## Phase 2: Game State ✅ COMPLETE

### Goal: Authoritative game state with simple movement

### Day 11-13: Game Engine Core
- [x] Implement `GameEngine` with tick loop (60Hz)
- [x] Create `GameState` struct with player positions
- [x] Add/remove players on connect/disconnect
- [x] Basic state snapshots

### Day 14-16: Input Processing
- [x] Define `PlayerInput` protobuf
- [x] Input queue per player
- [x] Process inputs each tick
- [x] Basic movement (position += velocity * dt)

### Day 17-18: State Broadcasting
- [x] Broadcast state snapshots (20Hz delta updates)
- [x] Protobuf serialization
- [x] Per-player delta detection (0.1 unit threshold)

### Day 19-20: Testing
- [x] Integration test with mock clients
- [x] Verify state consistency (18 tests passing)
- [x] Measure latency/bandwidth (benchmarks: ~102ns/input, ~107ns/tick)

### Deliverable
```bash
✅ Multiple clients can connect, see each other via state broadcasts
✅ Server runs at 60Hz tick, 20Hz state broadcast
```

---

## Phase 2.5: WebSocket Browser Client ✅ COMPLETE (2026-02-20)

### Goal: Browser-based canvas client with real-time multiplayer

### What We Built
- [x] Web bridge (cmd/webbridge) - WebSocket to UDP gateway on port 8081
- [x] Canvas rendering with glow effects and smooth 60fps interpolation
- [x] Real-time multiplayer - each browser tab gets unique player ID
- [x] Input handling - WASD/Arrow keys with mobile touch support
- [x] Player tracking by ID (not address) for webbridge multi-tenancy

### Architecture
```
Browser (WebSocket) ↔ Webbridge (port 8081) ↔ Game Server (UDP 9000)
                         ↓
              One UDP socket per browser tab (unique player ID)
              Broadcasts state deltas to all connected clients
```

### Lessons Learned
- Players from webbridge all come from same IP:port (127.0.0.1:XXXXX)
- Must track players by ID, not by address
- Webbridge translates between WebSocket and UDP protobuf protocols
- Broadcast delta updates to all connected browsers, not just the sender

### Deliverable
```bash
# Run both servers:
./bin/server         # UDP game server on :9000
./bin/webbridge      # WebSocket bridge on :8081

# Open http://localhost:8081 in multiple tabs
# Each tab is a unique player, all see each other moving in real-time
```

---

## Phase 3: Game Rooms & LiveKit Voice/Video

### Goal: Players can create a game room and share a link with friends. Everyone can see and hear each other while in the room (before and during game).

### Architecture
```
Create Game → Get unique room ID → Share link: localhost:8081/room/{43-char-id}
                                          ↓
                            Friends join same room → LiveKit video grid
                            Everyone sees/hears each other immediately
```

### Day 1-2: Room Management ✅ COMPLETE
- [x] Generate unique room IDs (43-char cryptographically secure)
- [x] Room lifecycle: create, join, leave, expire (1 min TTL)
- [x] In-memory room registry
- [x] Max 8 players per room (configurable)
- [x] Per-room game server spawning (true isolation)

### Day 3-4: HTTP API ✅ COMPLETE
- [x] `POST /rooms` → create room, returns `{roomId, joinLink}`
- [x] `GET /rooms/{id}` → room info (player count, status)
- [x] `DELETE /rooms/{id}` → close room

### Day 5-7: LiveKit Integration ✅ COMPLETE
- [x] LiveKit server config (livekit.yaml)
- [x] Generate LiveKit tokens per player join (`POST /livekit/token`)
- [x] Create room on first player join
- [x] Video grid UI component in browser
- [x] Mute/camera toggle controls
- [x] Auto-join video room when entering game room

### Day 8-9: Webbridge Updates ✅ COMPLETE
- [x] URL routing: `/room/{roomId}` → join specific room
- [x] WebSocket joins correct room context
- [x] Room isolation (players only see others in same room)
- [x] Landing page: "Create Room" button → generates shareable link

### Day 10-11: UI Polish ⏳ TODO
- [ ] Room lobby screen with video grid before game starts
- [ ] "Share Link" button with better UX
- [ ] Player list with mute/camera status icons
- [ ] "Start Game" when ready (voice/video continues during game)

### Deliverable
```bash
# Run full stack:
./run.sh   # Starts LiveKit (Docker) + WebBridge

# Player creates room:
curl -X POST http://localhost:8081/rooms
# → {"roomId": "xK9mN2pQ7vR3wY8zA1bC...", "joinLink": "http://localhost:8081/room/..."}

# Share link with friends
# Everyone opens the link → video grid appears → see and hear each other
# Game runs in same view → voice/video in corner panel
```

### LiveKit Setup ✅ DONE
```bash
# Self-hosted via Docker (automated in run.sh)
docker run -d \
  -v $PWD/livekit.yaml:/livekit.yaml \
  -p 7880:7880 \
  -p 7881:7881 \
  -p 50000-50200:50000-50200/udp \
  livekit/livekit-server:latest \
  --config /livekit.yaml

# API keys configured in livekit.yaml + environment variables
```

---

## Phase 4: Polish & Production

### Goal: Production-ready, deployable game server

### Day 9-11: Reliability
- [ ] Graceful reconnection (30s grace period)
- [ ] State sync on rejoin
- [ ] Input validation (speed hack detection)
- [ ] Rate limiting per player

### Day 12-13: Optimization
- [ ] Delta compression (bit-packing, quantized positions)
- [ ] Bandwidth monitoring
- [ ] Connection quality indicators

### Day 14-15: Deployment
- [ ] Docker compose for server + webbridge
- [ ] Environment config (PORT, ROOM_TTL, MAX_PLAYERS)
- [ ] Health check endpoints
- [ ] Deploy to Render.com or similar

---

## Success Metrics

| Milestone | Status |
|-----------|--------|
| Phase 1 | ✅ UDP echo working |
| Phase 2 | ✅ Multiplayer state sync at 60Hz |
| Phase 2.5 | ✅ Browser canvas client with real-time multiplayer |
| Phase 3 | ✅ Game rooms + LiveKit voice/video (UI polish remaining) |
| Phase 4 | ⏳ Polish & production deployment |

## Benchmarks

```
BenchmarkEngineTick-6       107.2 ns/op     0 B/op    0 allocs/op
BenchmarkDeltaCompute-6     18.1 µs/op      10752 B/op  213 allocs/op  (100 players)
BenchmarkApplyInput-6       101.7 ns/op     0 B/op    0 allocs/op
```

## Dependencies

```go
// go.mod
require (
    github.com/google/uuid v1.6.0  ✅ (room IDs, player IDs)
    google.golang.org/protobuf v1.36.0  ✅
    github.com/gorilla/websocket v1.5.3  ✅ (webbridge)
    github.com/livekit/server-sdk-go/v2 v2.13.3  ✅ (LiveKit tokens)
    github.com/livekit/protocol v1.44.0  ✅ (LiveKit protocol)
    
    // Future (not needed yet):
    // github.com/redis/go-redis/v9  (if room persistence needed)
)
```