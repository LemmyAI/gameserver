# GameServer Build Plan

## Phase 1: Foundation (Week 1-2)

### Goal: UDP echo server that can send/receive messages

### Day 1-2: Project Setup
- [ ] Initialize Go module
- [ ] Create directory structure per SPEC.md
- [ ] Set up Makefile for build/test
- [ ] Add Dockerfile for development

### Day 3-5: Transport Layer
- [ ] Define `Transport` interface in `internal/transport/transport.go`
- [ ] Implement `UDPTransport` with basic send/receive
- [ ] Implement `MockTransport` for testing
- [ ] Write unit tests for transport

### Day 6-7: Message Protocol
- [ ] Define protobuf messages in `proto/game.proto`
- [ ] Generate Go code with protoc
- [ ] Create `internal/protocol/` for encoding/decoding
- [ ] Test serialization performance

### Day 8-10: Basic Server
- [ ] Create `cmd/server/main.go`
- [ ] HTTP server with Fiber (health check endpoint)
- [ ] UDP listener that echoes messages
- [ ] Graceful shutdown handling

### Deliverable
```bash
# Run server
./gameserver

# Send UDP message, get echo back
echo "hello" | nc -u localhost 9000
```

---

## Phase 2: Game State (Week 3-4)

### Goal: Authoritative game state with simple movement

### Day 11-13: Game Engine Core
- [ ] Implement `GameEngine` with tick loop (60Hz)
- [ ] Create `GameState` struct with player positions
- [ ] Add/remove players on connect/disconnect
- [ ] Basic state snapshots

### Day 14-16: Input Processing
- [ ] Define `PlayerInput` protobuf
- [ ] Input queue per player
- [ ] Process inputs each tick
- [ ] Basic movement (position += velocity * dt)

### Day 17-18: State Broadcasting
- [ ] Broadcast state snapshots every tick
- [ ] Protobuf serialization
- [ ] Per-player delta detection (prepare for compression)

### Day 19-20: Testing
- [ ] Integration test with mock clients
- [ ] Verify state consistency
- [ ] Measure latency/bandwidth

### Deliverable
```bash
# Two clients can connect, see each other move
# State updates at 60Hz
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
- [ ] Implement `DeltaCompressor`
- [ ] Track last state per player
- [ ] Send only changed fields

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

| Milestone | Success Criteria |
|-----------|------------------|
| Phase 1 | UDP echo working |
| Phase 2 | 2 players can move and see each other |
| Phase 3 | Full auth → match → play flow |
| Phase 4 | 100 concurrent players, < 50ms latency |

## Dependencies

```go
// go.mod
require (
    github.com/gofiber/fiber/v3 v3.0.0
    github.com/livekit/server-sdk-go/v2 v2.0.0
    github.com/redis/go-redis/v9 v9.3.0
    github.com/jackc/pgx/v5 v5.5.0
    github.com/golang/protobuf v1.5.3
    github.com/google/uuid v1.5.0
)
```

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| UDP performance | Profile early, optimize hot paths |
| State sync issues | Extensive integration tests |
| LiveKit integration | Start simple, add voice last |
| Scale issues | Horizontal scaling from day 1 |
