# GameServer - Specification v0.3

## Overview

GameServer is a multiplayer game backend with a **hybrid networking approach**:
- **Game state over UDP/QUIC** - Low latency, purpose-built for games
- **Voice/video via Pion WebRTC** - Native WebRTC in Go, no external service
- **HTTP for auth/matchmaking** - Simple, reliable

This architecture uses each transport for what it does best.

## Philosophy

- **Right tool for the job** - UDP for game state, WebRTC for media, HTTP for APIs
- **Pluggable transport** - Abstract networking layer for flexibility
- **Server authority** - Authoritative game simulation with client prediction
- **Minimal complexity** - Start simple, optimize when needed

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        HTTP API                              │
│   (auth, matchmaking, leaderboards, stats)                  │
└────────────────────────────┬────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────┐
│                    Game Server                               │
│   ┌─────────────────┐    ┌─────────────────┐                │
│   │  UDP/QUIC       │    │  Pion WebRTC    │                │
│   │  Game State     │    │  Voice/Video    │                │
│   │  (Authoritative)│    │  (Relay)        │                │
│   └────────┬────────┘    └────────┬────────┘                │
│            │                      │                          │
│   ┌────────▼──────────────────────▼────────┐                │
│   │           Game Engine                   │                │
│   │   (State machine, validation, physics)  │                │
│   └────────────────────┬───────────────────┘                │
└─────────────────────────┼───────────────────────────────────┘
                          │
                 ┌────────▼────────┐
                 │   PostgreSQL    │
                 │   Redis Cache   │
                 └─────────────────┘

Client Connection:
┌─────────────┐         ┌─────────────┐
│   Player    │◄─UDP───►│ Game Server │ (game state, ~16ms)
│             │         │             │
│             │◄─WebRTC─►│ Pion WebRTC │ (voice/video)
└─────────────┘         └─────────────┘
```

## Repository Structure

```
gameserver/
├── cmd/
│   ├── server/             # Main server (HTTP + UDP)
│   └── authority/          # Game authority service
├── internal/
│   ├── transport/          # Network abstraction
│   │   ├── transport.go    # Interface definition
│   │   ├── udp.go          # UDP implementation
│   │   ├── quic.go         # QUIC implementation (optional)
│   │   └── mock.go         # For testing
│   ├── game/
│   │   ├── engine.go       # Game engine core
│   │   ├── state.go        # State machine
│   │   ├── prediction.go   # Client prediction support
│   │   └── validation.go   # Input validation, anti-cheat
│   ├── webrtc/             # Native WebRTC via Pion (voice/video)
│   │   ├── client.go       # SDK client
│   │   └── tokens.go       # Token generation
│   ├── matchmaker/
│   │   └── queue.go        # Matchmaking queue
│   └── auth/
│       └── auth.go         # Authentication
├── api/
│   └── handlers/           # HTTP handlers
├── proto/
│   └── game.proto          # Protocol definitions
├── db/
│   └── migrations/
└── docker/
```

## Core Components

### 1. Transport Abstraction

**Interface-based design** - swap implementations without touching game logic:

```go
// internal/transport/transport.go
package transport

type Transport interface {
    // Connection management
    Listen(addr string) error
    Close() error
    
    // Message I/O
    SendUnreliable(addr string, data []byte) error
    SendReliable(addr string, data []byte) error
    OnMessage(handler func(addr string, data []byte, reliable bool))
    
    // Connection state
    OnConnect(handler func(addr string))
    OnDisconnect(handler func(addr string))
}

// Config for different transport implementations
type Config struct {
    MaxMessageSize    int
    SendBufferSize    int
    RecvBufferSize    int
}
```

```go
// internal/transport/udp.go
package transport

type UDPTransport struct {
    conn    *net.UDPConn
    handlers *HandlerRegistry
}

func (t *UDPTransport) SendUnreliable(addr string, data []byte) error {
    // Pure UDP - fast, unreliable
    udpAddr, _ := net.ResolveUDPAddr("udp", addr)
    _, err := t.conn.WriteToUDP(data, udpAddr)
    return err
}

func (t *UDPTransport) SendReliable(addr string, data []byte) error {
    // UDP with ack/retry for important messages
    return t.sendWithAck(addr, data, 3, 100*time.Millisecond)
}
```

### 2. Game Engine

```go
// internal/game/engine.go
package game

type Engine struct {
    tickRate    int           // 60 ticks/sec
    state       *GameState
    transport   transport.Transport
    validator   *InputValidator
    predictor   *PredictionEngine
}

func (e *Engine) Run() {
    ticker := time.NewTicker(time.Second / time.Duration(e.tickRate))
    
    for {
        select {
        case <-ticker.C:
            e.processInputs()
            e.simulate()
            e.broadcastState()
        case msg := <-e.transport.Messages():
            e.handleMessage(msg)
        }
    }
}

func (e *Engine) processInputs() {
    for playerID, inputs := range e.pendingInputs {
        for _, input := range inputs {
            if e.validator.Validate(input, e.state.GetPlayer(playerID)) {
                e.state.ApplyInput(playerID, input)
            }
        }
    }
    e.pendingInputs = make(map[string][]PlayerInput)
}

func (e *Engine) broadcastState() {
    state := e.state.Snapshot()
    data, _ := proto.Marshal(state)
    
    // Broadcast to all connected players
    for _, player := range e.state.Players() {
        e.transport.SendUnreliable(player.Addr, data)
    }
}
```

### 3. Input Validation & Anti-Cheat

```go
// internal/game/validation.go
package game

type InputValidator struct {
    maxSpeed      float64
    maxTurnRate   float64
    history       map[string]*InputHistory
}

type InputHistory struct {
    inputs     []TimedInput
    lastPos    Position
    lastTime   time.Time
}

func (v *InputValidator) Validate(input PlayerInput, player *Player) error {
    history := v.history[player.ID]
    
    // Speed check
    if input.MoveSpeed > v.maxSpeed*1.05 { // 5% tolerance
        return ErrSpeedHack
    }
    
    // Teleport check
    dist := distance(history.lastPos, input.Position)
    elapsed := time.Since(history.lastTime).Seconds()
    if dist/elapsed > v.maxSpeed*2 {
        return ErrTeleport
    }
    
    // Rate check
    if len(history.inputs) > 120 { // 2 seconds at 60Hz
        recent := history.inputs[len(history.inputs)-120:]
        if timeBetween(recent) < 10*time.Millisecond {
            return ErrInputSpam
        }
    }
    
    history.lastPos = input.Position
    history.lastTime = time.Now()
    return nil
}
```

### 4. WebRTC Integration (Voice/Video via Pion)

**Native WebRTC in Go - no external service needed!**

```go
// internal/webrtc/webrtc.go
package webrtc

import (
    "github.com/pion/webrtc/v4"
)

type Manager struct {
    rooms map[string]*Room
}

func (m *Manager) CreatePeerConnection(roomID, playerID string) (*webrtc.PeerConnection, error) {
    config := webrtc.Configuration{
        ICEServers: []webrtc.ICEServer{
            {URLs: []string{"stun:stun.l.google.com:19302"}},
        },
    }
    return webrtc.NewPeerConnection(config)
}
```

### 5. Matchmaker

```go
// internal/matchmaker/queue.go
package matchmaker

type Matchmaker struct {
    queues    map[string]*Queue
    redis     *redis.Client
}

type Match struct {
    ID       string
    Players  []string
    ServerAddr string
    VoiceRoom  string
}

func (m *Matchmaker) FindMatch(gameType string, playerID string, skill int) (*Match, error) {
    queue := m.queues[gameType]
    
    // Skill-based matching
    candidate := queue.FindMatch(skill, 100) // ±100 skill
    
    if candidate != nil {
        matchID := uuid.New().String()
        
        // Assign game server
        server := m.assignServer()
        
        // Create WebRTC room for voice
        voiceRoom := "voice-" + matchID
        m.webrtc.CreateRoom(voiceRoom)
        
        return &Match{
            ID:         matchID,
            Players:    []string{candidate.PlayerID, playerID},
            ServerAddr: server.UDPAddr,
            VoiceRoom:  voiceRoom,
        }, nil
    }
    
    // Add to queue
    queue.Add(playerID, skill)
    return nil, ErrWaitingForMatch
}
```

## Protocol Definitions

```protobuf
// proto/game.proto
syntax = "proto3";

package gameserver;

// Player input (client -> server)
message PlayerInput {
    uint32 sequence = 1;
    uint64 timestamp = 2;
    
    // Movement
    float move_x = 3;
    float move_y = 4;
    
    // Actions
    repeated Action actions = 5;
}

message Action {
    uint32 type = 1;  // JUMP, SHOOT, INTERACT, etc.
    float target_x = 2;
    float target_y = 3;
}

// Game state (server -> client)
message GameState {
    uint32 tick = 1;
    uint64 timestamp = 2;
    repeated EntityState entities = 3;
    repeated PlayerState players = 4;
}

message EntityState {
    uint32 id = 1;
    float x = 2;
    float y = 3;
    float rotation = 4;
    uint32 state = 5;  // Animation state
}

message PlayerState {
    string id = 1;
    float x = 2;
    float y = 3;
    float health = 4;
    uint32 score = 5;
}

// Reliable messages
message ChatMessage {
    string from = 1;
    string text = 2;
    uint64 timestamp = 3;
}

message MatchEvent {
    oneof event {
        MatchStart start = 1;
        MatchEnd end = 2;
        PlayerJoin join = 3;
        PlayerLeave leave = 4;
    }
}
```

## HTTP API

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `POST /auth/login` | POST | Player authentication |
| `POST /auth/register` | POST | New player registration |
| `GET /auth/token` | GET | Get game server auth token |
| `GET /auth/voice-token` | GET | Get WebRTC signaling info |
| `POST /match/queue` | POST | Join matchmaking queue |
| `GET /match/status` | GET | Check queue status |
| `GET /stats/{player}` | GET | Player statistics |
| `GET /leaderboard` | GET | Game leaderboard |

## DataChannel Topics (for future WebRTC fallback)

| Topic | Mode | Purpose |
|-------|------|---------|
| `voice` | Built-in | Voice chat |
| `video` | Built-in | Video chat |

## Tech Stack

| Component | Technology |
|-----------|------------|
| **Language** | Go 1.21+ |
| **Transport** | UDP (primary), QUIC (optional) |
| **Media** | Pion WebRTC (voice/video only) |
| **Database** | PostgreSQL 15+ |
| **Cache** | Redis 7+ |
| **Protocol** | Protocol Buffers |
| **Monitoring** | Prometheus + Grafana |

## Configuration

```yaml
# config.yaml
server:
  http_addr: ":8080"
  udp_addr: ":9000"
  
transport:
  type: udp  # udp or quic
  max_message_size: 1400
  send_buffer: 1024
  recv_buffer: 1024
  
webrtc:
  stun_servers:
    - "stun:stun.l.google.com:19302"
    - "stun:stun1.l.google.com:19302"
  
database:
  url: "${DATABASE_URL}"
  
redis:
  url: "${REDIS_URL}"
  
game:
  tick_rate: 60
  max_players_per_room: 16
```

### 6. Reliable UDP Implementation

```go
// internal/transport/reliable.go
package transport

type ReliableSender struct {
    pending     map[uint32]*PendingPacket
    nextSeq     uint32
    maxRetries  int
    retryDelay  time.Duration
}

type PendingPacket struct {
    seq       uint32
    data      []byte
    addr      string
    sentAt    time.Time
    retries   int
}

func (r *ReliableSender) Send(addr string, data []byte) error {
    seq := r.nextSeq
    r.nextSeq++
    
    // Wrap with sequence number
    packet := Packet{
        Seq:  seq,
        Type: PacketTypeReliable,
        Data: data,
    }
    
    // Store for retry
    r.pending[seq] = &PendingPacket{
        seq:     seq,
        data:    packet.Marshal(),
        addr:    addr,
        sentAt:  time.Now(),
        retries: 0,
    }
    
    return r.sendRaw(addr, packet.Marshal())
}

func (r *ReliableSender) OnAck(seq uint32) {
    delete(r.pending, seq)
}

func (r *ReliableSender) RetryLoop() {
    ticker := time.NewTicker(r.retryDelay)
    for range ticker.C {
        now := time.Now()
        for seq, p := range r.pending {
            if now.Sub(p.sentAt) > r.retryDelay {
                if p.retries >= r.maxRetries {
                    delete(r.pending, seq)
                    continue
                }
                r.sendRaw(p.addr, p.data)
                p.sentAt = now
                p.retries++
            }
        }
    }
}
```

### 7. Delta Compression

```go
// internal/game/delta.go
package game

type DeltaCompressor struct {
    lastState  map[string]*EntityState
    maxDelta   int // Max entities per delta
}

type StateDelta struct {
    BaseTick   uint32
    Created    []EntityState
    Updated    []EntityDelta
    Removed    []uint32
}

type EntityDelta struct {
    ID         uint32
    Fields     []FieldDelta
}

type FieldDelta struct {
    FieldID    uint8
    Value      interface{}
}

func (d *DeltaCompressor) Compress(fullState *GameState) []byte {
    delta := &StateDelta{
        BaseTick: fullState.Tick - 1,
    }
    
    for _, entity := range fullState.Entities {
        last, exists := d.lastState[entity.ID]
        
        if !exists {
            // New entity
            delta.Created = append(delta.Created, *entity)
        } else {
            // Check what changed
            eDelta := EntityDelta{ID: entity.ID}
            
            if entity.X != last.X {
                eDelta.Fields = append(eDelta.Fields, 
                    FieldDelta{FieldID: FieldX, Value: entity.X})
            }
            if entity.Y != last.Y {
                eDelta.Fields = append(eDelta.Fields, 
                    FieldDelta{FieldID: FieldY, Value: entity.Y})
            }
            // ... other fields
            
            if len(eDelta.Fields) > 0 {
                delta.Updated = append(delta.Updated, eDelta)
            }
        }
    }
    
    // Check for removed entities
    for id := range d.lastState {
        if _, exists := fullState.Entities[id]; !exists {
            delta.Removed = append(delta.Removed, id)
        }
    }
    
    // Update last state
    d.lastState = make(map[string]*EntityState)
    for _, e := range fullState.Entities {
        d.lastState[e.ID] = e
    }
    
    return delta.Marshal()
}
```

### 8. Player Reconnection

```go
// internal/game/reconnect.go
package game

type ReconnectionManager struct {
    sessions    map[string]*PlayerSession
    timeout     time.Duration
    redis       *redis.Client
}

type PlayerSession struct {
    PlayerID    string
    MatchID     string
    LastSeen    time.Time
    State       *PlayerState
    Addr        string
}

func (r *ReconnectionManager) OnDisconnect(playerID string) {
    session, exists := r.sessions[playerID]
    if !exists {
        return
    }
    
    // Store session for reconnection
    session.LastSeen = time.Now()
    r.redis.Set(ctx, "session:"+playerID, session, r.timeout)
    
    // Don't remove from game immediately - give time to reconnect
    go r.waitForReconnect(session)
}

func (r *ReconnectionManager) waitForReconnect(session *PlayerSession) {
    deadline := time.Now().Add(r.timeout)
    
    for time.Now().Before(deadline) {
        // Check if player reconnected
        if newSession, err := r.redis.Get(ctx, "session:"+session.PlayerID).Result(); err == nil {
            // Player reconnected elsewhere
            r.OnReconnect(session.PlayerID, newSession.Addr)
            return
        }
        time.Sleep(100 * time.Millisecond)
    }
    
    // Timeout - remove from game
    r.removePlayer(session.PlayerID)
}

func (r *ReconnectionManager) OnReconnect(playerID, newAddr string) *PlayerSession {
    data, err := r.redis.Get(ctx, "session:"+playerID).Bytes()
    if err != nil {
        return nil // Session expired
    }
    
    var session PlayerSession
    json.Unmarshal(data, &session)
    
    // Update address
    session.Addr = newAddr
    session.LastSeen = time.Now()
    
    // Send full state sync to catch up
    r.sendFullStateSync(playerID)
    
    return &session
}

func (r *ReconnectionManager) sendFullStateSync(playerID string) {
    // Send complete game state to reconnecting player
    // They may have missed updates during disconnect
}
```

## Version Goals

### v0.3 (Current)
- [ ] UDP transport implementation
- [ ] Transport abstraction interface
- [ ] HTTP auth endpoints
- [ ] Basic game state management
- [ ] Input validation
- [ ] WebRTC signaling endpoint
- [ ] Reliable UDP with ACK/retry

### v0.4
- [ ] Matchmaking queue
- [ ] Client prediction support
- [ ] Delta compression for state
- [ ] Player reconnection handling
- [ ] Leaderboards

### v1.0
- [ ] QUIC transport option
- [ ] Advanced anti-cheat
- [ ] Horizontal scaling
- [ ] Spectator mode
- [ ] Replay system

## Testing Strategy

```go
// Use mock transport for testing
func TestGameEngine(t *testing.T) {
    mock := transport.NewMock()
    engine := game.NewEngine(mock, game.Config{TickRate: 60})
    
    // Simulate player input
    mock.SimulateMessage("player1:1234", &PlayerInput{
        Sequence: 1,
        MoveX: 1.0,
    })
    
    // Run ticks
    engine.Tick()
    engine.Tick()
    
    // Verify state broadcast
    msgs := mock.SentMessages("player1:1234")
    assert.Len(t, msgs, 2)
}
```

## Deployment

### Docker Compose

```yaml
version: '3.8'
services:
  game-server:
    build: .
    ports:
      - "8080:8080"   # HTTP
      - "9000:9000/udp"  # UDP game traffic
    environment:
      - DATABASE_URL=postgres://...
      - REDIS_URL=redis://redis:6379
      - STUN_SERVERS=stun:stun.l.google.com:19302
    depends_on:
      - postgres
      - redis
      
  postgres:
    image: postgres:15-alpine
    
  redis:
    image: redis:7-alpine
```

## Key Changes from v0.2

| Aspect | v0.2 | v0.3 |
|--------|------|------|
| **Transport** | External service | UDP primary, Pion WebRTC for media |
| **Architecture** | Coupled to LiveKit | Pluggable transport interface |
| **Game traffic** | DataChannels | Pure UDP |
| **Voice/video** | External service | Native Pion WebRTC |
| **Flexibility** | Vendor locked | Self-contained |

## License

MIT License

## Version History

- **v0.3** (2026): Hybrid architecture - UDP for game, Pion WebRTC for media
- **v0.2** (2026): LiveKit-only architecture (archived)
- **v0.1** (Archived): Initial Rust/Node specs
