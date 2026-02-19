# GameServer - Specification v0.3

## Overview

GameServer is a multiplayer game backend with a **hybrid networking approach**:
- **Game state over UDP/QUIC** - Low latency, purpose-built for games
- **Voice/video over LiveKit** - Battle-tested WebRTC infrastructure
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
│   │  UDP/QUIC       │    │  LiveKit        │                │
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
│             │◄─WebRTC─►│  LiveKit    │ (voice/video)
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
│   ├── livekit/            # LiveKit integration (voice/video only)
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

### 4. LiveKit Integration (Voice/Video Only)

```go
// internal/livekit/tokens.go
package livekit

import (
    "github.com/livekit/protocol/auth"
)

type TokenGenerator struct {
    apiKey    string
    apiSecret string
}

// Generate token for voice/video room
func (t *TokenGenerator) GenerateVoiceToken(playerID, roomName string) (string, error) {
    at := auth.NewAccessToken(t.apiKey, t.apiSecret)
    
    grant := &auth.VideoGrant{
        RoomJoin: true,
        Room:     roomName,
        CanPublish: true,
        CanSubscribe: true,
    }
    
    at.AddGrant(grant).
        SetIdentity(playerID).
        SetValidFor(time.Hour)
    
    return at.ToJWT()
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
        
        // Create LiveKit room for voice
        voiceRoom := "voice-" + matchID
        m.livekit.CreateRoom(voiceRoom)
        
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
| `GET /auth/voice-token` | GET | Get LiveKit voice token |
| `POST /match/queue` | POST | Join matchmaking queue |
| `GET /match/status` | GET | Check queue status |
| `GET /stats/{player}` | GET | Player statistics |
| `GET /leaderboard` | GET | Game leaderboard |

## DataChannel Topics (for future LiveKit fallback)

| Topic | Mode | Purpose |
|-------|------|---------|
| `voice` | Built-in | Voice chat |
| `video` | Built-in | Video chat |

## Tech Stack

| Component | Technology |
|-----------|------------|
| **Language** | Go 1.21+ |
| **Transport** | UDP (primary), QUIC (optional) |
| **Media** | LiveKit (voice/video only) |
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
  
livekit:
  host: "https://your-livekit.cloud"
  api_key: "${LIVEKIT_API_KEY}"
  api_secret: "${LIVEKIT_API_SECRET}"
  
database:
  url: "${DATABASE_URL}"
  
redis:
  url: "${REDIS_URL}"
  
game:
  tick_rate: 60
  max_players_per_room: 16
```

## Version Goals

### v0.3 (Current)
- [ ] UDP transport implementation
- [ ] Transport abstraction interface
- [ ] HTTP auth endpoints
- [ ] Basic game state management
- [ ] Input validation
- [ ] LiveKit voice token generation

### v0.4
- [ ] Matchmaking queue
- [ ] Client prediction support
- [ ] Delta compression for state
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
      - LIVEKIT_API_KEY=${LIVEKIT_API_KEY}
      - LIVEKIT_API_SECRET=${LIVEKIT_API_SECRET}
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
| **Transport** | LiveKit-only | UDP primary, LiveKit for media |
| **Architecture** | Coupled to LiveKit | Pluggable transport interface |
| **Game traffic** | DataChannels | Pure UDP |
| **Voice/video** | DataChannels | LiveKit (proper use) |
| **Flexibility** | Vendor locked | Can swap transports |

## License

MIT License

## Version History

- **v0.3** (2026): Hybrid architecture - UDP for game, LiveKit for media
- **v0.2** (2026): LiveKit-only architecture (archived)
- **v0.1** (Archived): Initial Rust/Node specs
