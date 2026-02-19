# GameServer - Specification v0.2

## Overview

GameServer is a multiplayer game backend that uses LiveKit as the sole communication layer. All real-time traffic flows through LiveKit DataChannels - no WebSocket, no custom protocols. The server acts as an authoritative participant in LiveKit rooms when needed.

## Philosophy

- **LiveKit-only** - All game traffic over DataChannels
- **UDP by default** - Low latency, configurable reliability
- **Server as participant** - Authority when needed, observer otherwise
- **Minimal API surface** - HTTP only for auth and persistence

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        HTTP API                              │
│   (auth, tokens, matchmaking, leaderboards, stats)          │
└────────────────────────────┬────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────┐
│                    LiveKit Cloud                             │
│   (signaling, TURN/STUN, SFU, rooms, recording)             │
└────────────────────────────┬────────────────────────────────┘
                             │
         ┌───────────────────┼───────────────────┐
         │                   │                   │
    ┌────▼────┐        ┌─────▼─────┐       ┌─────▼─────┐
    │ Player  │◄──────►│  Game     │◄──────►│  Player   │
    │    A    │   DC   │  Server   │  DC   │     B     │
    └─────────┘        │ (Authority)│      └───────────┘
                       └───────────┘
                             │
                    ┌────────▼────────┐
                    │   PostgreSQL    │
                    │   Redis Cache   │
                    └─────────────────┘
```

## Repository Structure

```
gameserver/
├── cmd/                    # Entry points
│   ├── api/                # HTTP API server
│   └── authority/          # LiveKit participant (game authority)
├── internal/
│   ├── livekit/            # LiveKit integration
│   │   ├── client.go       # LiveKit SDK client
│   │   ├── room.go         # Room management
│   │   ├── participant.go  # Server participant
│   │   └── egress/         # Recording/webhooks
│   ├── game/               # Game engine core (Rust FFI or Go)
│   │   ├── state.go        # Game state machine
│   │   ├── authority.go    # Authoritative simulation
│   │   ├── validation.go   # Input validation, anti-cheat
│   │   └── rollback.go     # Rollback netcode support
│   ├── protocols/          # Protocol handlers
│   │   ├── game.go         # Game state/input
│   │   ├── chat.go         # Chat messages
│   │   └── match.go        # Match events
│   ├── matchmaker/         # Matchmaking queue
│   ├── leaderboard/        # Leaderboard service
│   └── auth/               # Authentication
├── api/                    # HTTP handlers
│   ├── auth.go
│   ├── match.go
│   ├── stats.go
│   └── tokens.go
├── db/                     # Database
│   ├── migrations/
│   └── queries/
├── config/
└── docker/
```

## Core Components

### 1. HTTP API Server

Minimal HTTP surface for non-real-time operations:

| Endpoint | Purpose |
|----------|---------|
| `POST /auth/login` | Player authentication |
| `POST /auth/register` | New player registration |
| `GET /auth/token` | Get LiveKit access token |
| `POST /match/queue` | Join matchmaking queue |
| `GET /match/status` | Check queue status |
| `GET /stats/{game}/{player}` | Player statistics |
| `GET /leaderboard/{game}` | Game leaderboard |

### 2. LiveKit Authority Service

The server joins LiveKit rooms as a participant with special privileges:

```go
// Server joins room as authority
func (a *Authority) JoinRoom(roomName, gameID string) error {
    // Connect as participant with admin permissions
    participant, err := a.lk.Join(roomName, WithAdminToken())
    if err != nil {
        return err
    }
    
    // Subscribe to all DataChannels
    participant.OnDataPacket(a.handleGamePacket)
    
    // Run game simulation loop
    go a.gameLoop(roomName, gameID)
    
    return nil
}
```

### 3. Game State Engine

Authoritative game simulation:

```go
type GameEngine struct {
    tickRate    int           // e.g., 60 ticks/sec
    state       *GameState
    inputs      map[string][]PlayerInput
    validator   *InputValidator
}

func (e *GameEngine) Run() {
    ticker := time.NewTicker(time.Second / time.Duration(e.tickRate))
    for range ticker.C {
        e.processInputs()
        e.simulate()
        e.broadcastState()
    }
}

func (e *GameEngine) processInputs() {
    // Validate all inputs
    for playerID, inputs := range e.inputs {
        for _, input := range inputs {
            if e.validator.Validate(input) {
                e.state.ApplyInput(playerID, input)
            }
        }
    }
    e.inputs = make(map[string][]PlayerInput)
}

func (e *GameEngine) broadcastState() {
    state := e.state.Snapshot()
    data, _ := proto.Marshal(state)
    
    // Broadcast via LiveKit DataChannel (unreliable for speed)
    e.lk.BroadcastData("state", data, DataPacketConfig{
        Reliable: false,
    })
}
```

### 4. Protocol Handlers

All communication uses Protocol Buffers over DataChannels:

```go
func (h *ProtocolHandler) handleGamePacket(pkt *livekit.DataPacket) {
    switch pkt.Topic {
    case "input":
        var input PlayerInput
        proto.Unmarshal(pkt.Payload, &input)
        h.engine.AddInput(pkt.ParticipantID, input)
        
    case "events":
        var event GameEvent
        proto.Unmarshal(pkt.Payload, &event)
        h.handleEvent(pkt.ParticipantID, event)
        
    case "chat":
        var msg ChatMessage
        proto.Unmarshal(pkt.Payload, &msg)
        h.handleChat(msg)
    }
}
```

### 5. Matchmaker

Queue-based matchmaking:

```go
type Matchmaker struct {
    queues     map[string]*Queue  // gameID -> queue
    matchTimeout time.Duration
}

func (m *Matchmaker) FindMatch(gameID, playerID string, skill int) (*Match, error) {
    queue := m.queues[gameID]
    
    // Skill-based matching with tolerance
    match := queue.Find(func(p *QueuedPlayer) bool {
        return abs(p.Skill-skill) < 100
    })
    
    if match != nil {
        // Create LiveKit room
        room, _ := m.lk.CreateRoom(gameID + "-" + uuid.New())
        
        // Return room info and tokens
        return &Match{
            RoomID: room.Name,
            Tokens: map[string]string{
                match.PlayerID: m.lk.Token(match.PlayerID, room),
                playerID:       m.lk.Token(playerID, room),
            },
        }, nil
    }
    
    // Add to queue
    queue.Add(playerID, skill)
    return nil, ErrWaitingForMatch
}
```

### 6. Leaderboard Service

```go
type Leaderboard struct {
    redis  *redis.Client
    db     *sql.DB
}

func (l *Leaderboard) Update(gameID, playerID string, score int) error {
    ctx := context.Background()
    
    // Update Redis sorted set (fast)
    l.redis.ZAdd(ctx, "lb:"+gameID, &redis.Z{
        Score:  float64(score),
        Member: playerID,
    })
    
    // Persist to PostgreSQL (async)
    go l.db.Exec(
        "INSERT INTO scores (game_id, player_id, score) VALUES ($1, $2, $3) ON CONFLICT UPDATE",
        gameID, playerID, score,
    )
    
    return nil
}

func (l *Leaderboard) GetRank(gameID, playerID string) (int, error) {
    return l.redis.ZRank(context.Background(), "lb:"+gameID, playerID).Result()
}
```

## DataChannel Topics

| Topic | Mode | Direction | Purpose |
|-------|------|-----------|---------|
| `input` | Unreliable, ordered | Client → Server | Player inputs |
| `state` | Unreliable | Server → Clients | Game state snapshots |
| `events` | Reliable | Bidirectional | Game events (kills, powerups) |
| `chat` | Reliable | Bidirectional | Chat messages |
| `voice` | Built-in | Bidirectional | Voice via LiveKit |
| `video` | Built-in | Bidirectional | Video via LiveKit |

## LiveKit Integration

### Token Generation

```go
func (s *AuthService) GenerateToken(playerID, roomName string, isServer bool) (string, error) {
    at := auth.NewAccessToken(s.lkAPIKey, s.lkAPISecret)
    
    grant := &auth.VideoGrant{
        RoomJoin: true,
        Room:     roomName,
    }
    
    if isServer {
        // Server gets admin privileges
        grant.RoomAdmin = true
        grant.CanPublishData = true
        grant.CanSubscribe = true
    } else {
        grant.CanPublishData = true
        grant.CanSubscribe = true
    }
    
    at.AddGrant(grant).
        SetIdentity(playerID).
        SetName(playerID).
        SetValidFor(time.Hour)
    
    return at.ToJWT()
}
```

### Room Creation

```go
func (s *RoomService) CreateGameRoom(gameID string) (*livekit.Room, error) {
    return s.lkClient.CreateRoom(context.Background(), &livekit.CreateRoomRequest{
        Name:            gameID + "-" + uuid.New().String()[:8],
        EmptyTimeout:    300,  // 5 min timeout
        MaxParticipants: 100,
        Metadata:        fmt.Sprintf(`{"game":"%s"}`, gameID),
    })
}
```

### Webhook Handling

```go
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
    event, _ := webhook.Receive(r, h.lkAPIKey)
    
    switch event.Event {
    case livekit.WebhookEventRoomStarted:
        log.Info("Room started", "room", event.Room.Name)
        
    case livekit.WebhookEventParticipantJoined:
        h.onPlayerJoin(event.Room.Name, event.Participant.Identity)
        
    case livekit.WebhookEventParticipantLeft:
        h.onPlayerLeave(event.Room.Name, event.Participant.Identity)
        
    case livekit.WebhookEventRoomFinished:
        h.onRoomEnd(event.Room.Name)
    }
}
```

## Anti-Cheat

### Input Validation

```go
type InputValidator struct {
    maxInputsPerTick int
    state            *GameState
}

func (v *InputValidator) Validate(input PlayerInput) bool {
    // Rate limit
    if len(input.Actions) > v.maxInputsPerTick {
        return false
    }
    
    // Movement speed check
    if input.MoveX > 1.0 || input.MoveX < -1.0 {
        return false
    }
    
    // Impossibility check (teleport detection)
    lastPos := v.state.GetPosition(input.PlayerID)
    newPos := calculateNewPos(lastPos, input)
    if distance(lastPos, newPos) > v.maxSpeed {
        return false // Impossible movement
    }
    
    return true
}
```

### Server Authority Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `full` | All state computed by server | Competitive games |
| `validated` | Client predicts, server validates | Fast-paced action |
| `observer` | Server only observes, no authority | Casual/party games |

## Database Schema

### PostgreSQL

```sql
-- Players
CREATE TABLE players (
    id UUID PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE,
    created_at TIMESTAMP DEFAULT NOW(),
    metadata JSONB
);

-- Scores
CREATE TABLE scores (
    id UUID PRIMARY KEY,
    game_id VARCHAR(100) NOT NULL,
    player_id UUID REFERENCES players(id),
    score INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    INDEX idx_game_score (game_id, score DESC)
);

-- Match history
CREATE TABLE matches (
    id UUID PRIMARY KEY,
    game_id VARCHAR(100) NOT NULL,
    room_id VARCHAR(100) NOT NULL,
    players UUID[] NOT NULL,
    winner UUID,
    started_at TIMESTAMP,
    ended_at TIMESTAMP,
    metadata JSONB
);
```

### Redis Keys

```
player:{id}:online       → "1" (TTL: 60s, heartbeat)
lb:{game_id}            → ZSET (leaderboard)
queue:{game_id}         → LIST (matchmaking queue)
room:{room_id}:players  → SET (active players)
```

## Tech Stack

| Component | Technology |
|-----------|------------|
| **Language** | Go (services) + Rust (game engine core) |
| **Runtime** | Go 1.21+ |
| **HTTP** | Chi or Fiber |
| **LiveKit SDK** | go-livekit |
| **Database** | PostgreSQL 15+ |
| **Cache** | Redis 7+ |
| **Protocol** | Protocol Buffers |
| **Container** | Docker + Compose |

## Configuration

```yaml
# config.yaml
server:
  http_port: 8080
  
livekit:
  host: "https://your-livekit.cloud"
  api_key: "${LIVEKIT_API_KEY}"
  api_secret: "${LIVEKIT_API_SECRET}"
  
database:
  url: "${DATABASE_URL}"
  
redis:
  url: "${REDIS_URL}"
  
game:
  default_tick_rate: 60
  authority_mode: "validated"
```

## Deployment

### Docker Compose

```yaml
version: '3.8'
services:
  api:
    build: ./cmd/api
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://...
      - REDIS_URL=redis://redis:6379
      - LIVEKIT_API_KEY=${LIVEKIT_API_KEY}
      - LIVEKIT_API_SECRET=${LIVEKIT_API_SECRET}
    depends_on:
      - postgres
      - redis
      
  authority:
    build: ./cmd/authority
    environment:
      - LIVEKIT_API_KEY=${LIVEKIT_API_KEY}
      - LIVEKIT_API_SECRET=${LIVEKIT_API_SECRET}
    depends_on:
      - redis
      
  postgres:
    image: postgres:15
    volumes:
      - pgdata:/var/lib/postgresql/data
      
  redis:
    image: redis:7-alpine
    volumes:
      - redisdata:/data

volumes:
  pgdata:
  redisdata:
```

## Version Goals

### v0.2 (Current)
- [ ] LiveKit integration
- [ ] HTTP auth/token endpoints
- [ ] Basic game authority service
- [ ] Protocol definitions
- [ ] Matchmaking queue

### v0.3
- [ ] Input validation/anti-cheat
- [ ] Leaderboard service
- [ ] Match history
- [ ] Webhook handling

### v1.0
- [ ] Full authority modes
- [ ] Rollback netcode support
- [ ] Replay recording (LiveKit Egress)
- [ ] Horizontal scaling

## License

MIT License

## Version History

- **v0.2** (2025): Unified LiveKit-only architecture
- **v0.1** (Archived): Initial Rust/Node specs
