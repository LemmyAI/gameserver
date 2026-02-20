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

## Phase 3: Matchmaking + Auth (Week 5-6)

### Goal: Players can find matches and authenticate

### Day 21-23: Authentication
- [ ] JWT token generation
- [ ] `/auth/login` endpoint
- [ ] `/auth/register` endpoint
- [ ] Token validation middleware

### Day 24-26: Matchmaking
- [ ] Redis-backed match queue
- [ ] `/match/queue` endpoint
- [ ] `/match/status` endpoint
- [ ] Simple skill-based pairing

### Day 27-28: Match Lifecycle
- [ ] Create game room on match found
- [ ] Assign UDP address to match
- [ ] Create LiveKit voice room
- [ ] Return connection details to players

### Day 29-30: Integration
- [ ] End-to-end match flow
- [ ] Player joins → queues → matches → plays
- [ ] Clean up on disconnect

### Deliverable
```bash
# Player flow:
# 1. POST /auth/login → get token
# 2. POST /match/queue → enter queue
# 3. GET /match/status → waiting/matched
# 4. Connect to UDP server address
# 5. Play game
```

---

## Phase 4: Polish (Week 7-8)

### Goal: Production-ready foundation

### Day 31-33: Reliable UDP
- [ ] Implement `ReliableSender` with ACK/retry
- [ ] Sequence numbers for important messages
- [ ] Timeout handling

### Day 34-35: Delta Compression
- [x] Basic delta detection implemented in Phase 2
- [ ] Bit-packing for smaller deltas
- [ ] Quantized positions (int16 instead of float32)

### Day 36-37: Reconnection
- [ ] Session persistence in Redis
- [ ] Grace period (30s) for reconnect
- [ ] State sync on rejoin

### Day 38-39: Input Validation
- [ ] Speed/teleport detection
- [ ] Rate limiting per player
- [ ] Log suspicious activity

### Day 40: Final Testing
- [ ] Load test with 100+ concurrent players
- [ ] Network simulation (packet loss, latency)
- [ ] Document API

---

## Success Metrics

| Milestone | Status |
|-----------|--------|
| Phase 1 | ✅ UDP echo working |
| Phase 2 | ✅ 2 players can move, state sync at 60Hz |
| Phase 3 | ⏳ Pending |
| Phase 4 | ⏳ Pending |

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
    github.com/gofiber/fiber/v3 v3.0.0  // Not yet used
    github.com/livekit/server-sdk-go/v2 v2.0.0  // Not yet used
    github.com/redis/go-redis/v9 v9.3.0  // Not yet used
    github.com/jackc/pgx/v5 v5.5.0  // Not yet used
    google.golang.org/protobuf v1.36.0  ✅
    github.com/google/uuid v1.6.0  ✅
)
```