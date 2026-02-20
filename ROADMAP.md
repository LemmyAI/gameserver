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

## Phase 3: Game Rooms & Shareable Links

### Goal: Players can create a game room and share a link with friends

### Architecture
```
Create Game → Get unique room ID → Share link: localhost:8081/room/ABC123
                                          ↓
                            Friends join same room, play together
```

### Day 1-2: Room Management
- [ ] Generate unique room IDs (short, shareable: ABC123 format)
- [ ] Room lifecycle: create, join, leave, expire (empty for 5 min)
- [ ] In-memory room registry (Redis later if needed)
- [ ] Max players per room (configurable, default 8)

### Day 3-4: HTTP API
- [ ] `POST /rooms` → create room, returns `{roomId, joinLink}`
- [ ] `GET /rooms/{id}` → room info (player count, status)
- [ ] `DELETE /rooms/{id}` → close room (host only)

### Day 5-6: Webbridge Updates
- [ ] URL routing: `/room/{roomId}` → join specific room
- [ ] WebSocket joins correct room context
- [ ] Room isolation (players only see others in same room)
- [ ] Landing page: "Create Game" button → generates shareable link

### Day 7-8: UI Polish
- [ ] Room lobby screen (waiting for players)
- [ ] "Share Link" button with clipboard copy
- [ ] Player list in room
- [ ] "Start Game" when ready

### Deliverable
```bash
# Player creates room:
curl -X POST http://localhost:8081/rooms
# → {"roomId": "ABC123", "link": "http://localhost:8081/room/ABC123"}

# Share link with friends
# Everyone opens the link → same game room → play together
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
| Phase 3 | ⏳ Game rooms & shareable links |
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
    
    // Future (not needed yet):
    // github.com/redis/go-redis/v9  (if room persistence needed)
)
```