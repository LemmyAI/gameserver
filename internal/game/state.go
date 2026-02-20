// Package game implements the authoritative game engine.
package game

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config holds game engine configuration.
type Config struct {
	TickRate       int     // Ticks per second (default: 60)
	MaxPlayers     int     // Maximum concurrent players (default: 100)
	PlayerSpeed    float32 // Units per second (default: 100)
	WorldWidth     float32 // World bounds (default: 1000)
	WorldHeight    float32 // World bounds (default: 1000)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		TickRate:    60,
		MaxPlayers:  100,
		PlayerSpeed: 100,
		WorldWidth:  1000,
		WorldHeight: 1000,
	}
}

// Player represents a connected player.
type Player struct {
	ID          string
	Name        string
	Addr        string      // UDP address
	Position    Vec2        // Current position
	Velocity    Vec2        // Current velocity
	LastInput   uint64      // Last processed input sequence
	LastSeen    time.Time   // Last message time
	ConnectedAt time.Time

	// Input queue for deterministic processing
	InputQueue []Input
}

// Vec2 is a 2D vector.
type Vec2 struct {
	X float32
	Y float32
}

// Input represents player input for a single tick.
type Input struct {
	Sequence  uint64
	Timestamp uint64
	Movement  Vec2  // -1 to 1 for each axis
	Jump      bool
	Action1   bool
	Action2   bool
}

// State represents the authoritative game state.
type State struct {
	mu      sync.RWMutex
	players map[string]*Player
	config  Config
	tick    uint64
	started time.Time
}

// NewState creates a new game state.
func NewState(config Config) *State {
	return &State{
		players: make(map[string]*Player),
		config:  config,
		started: time.Now(),
	}
}

// AddPlayer creates and adds a new player.
func (s *State) AddPlayer(name, addr string) *Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check max players
	if len(s.players) >= s.config.MaxPlayers {
		return nil
	}

	player := &Player{
		ID:          uuid.New().String()[:8], // Short ID
		Name:        name,
		Addr:        addr,
		Position:    Vec2{X: s.config.WorldWidth / 2, Y: s.config.WorldHeight / 2}, // Spawn center
		Velocity:    Vec2{X: 0, Y: 0},
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
		InputQueue:  make([]Input, 0, 16), // Pre-allocate input queue
	}

	s.players[player.ID] = player
	return player
}

// AddPlayerWithID creates and adds a new player with a specific ID.
func (s *State) AddPlayerWithID(name, playerID, addr string) *Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if player ID already exists
	if _, exists := s.players[playerID]; exists {
		return nil
	}

	// Check max players
	if len(s.players) >= s.config.MaxPlayers {
		return nil
	}

	player := &Player{
		ID:          playerID,
		Name:        name,
		Addr:        addr,
		Position:    Vec2{X: s.config.WorldWidth / 2, Y: s.config.WorldHeight / 2}, // Spawn center
		Velocity:    Vec2{X: 0, Y: 0},
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
		InputQueue:  make([]Input, 0, 16), // Pre-allocate input queue
	}

	s.players[player.ID] = player
	return player
}

// RemovePlayer removes a player by ID.
func (s *State) RemovePlayer(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.players, id)
}

// RemovePlayerByAddr removes a player by address.
func (s *State) RemovePlayerByAddr(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, p := range s.players {
		if p.Addr == addr {
			delete(s.players, id)
			return
		}
	}
}

// GetPlayer returns a player by ID.
func (s *State) GetPlayer(id string) *Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.players[id]
}

// GetPlayerByAddr returns a player by address.
func (s *State) GetPlayerByAddr(addr string) *Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.players {
		if p.Addr == addr {
			return p
		}
	}
	return nil
}

// AllPlayers returns all players (for broadcasting).
func (s *State) AllPlayers() []*Player {
	s.mu.RLock()
	defer s.mu.RUnlock()

	players := make([]*Player, 0, len(s.players))
	for _, p := range s.players {
		players = append(players, p)
	}
	return players
}

// PlayerCount returns the current player count.
func (s *State) PlayerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.players)
}

// Tick increments the tick counter.
func (s *State) Tick() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick++
	return s.tick
}

// CurrentTick returns the current tick.
func (s *State) CurrentTick() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tick
}

// Config returns the game configuration.
func (s *State) Config() Config {
	return s.config
}

// ApplyInput queues player input for processing on next tick.
// Returns false if input is stale (already processed).
func (s *State) ApplyInput(playerID string, input Input) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	player, ok := s.players[playerID]
	if !ok {
		return false
	}

	// Skip if we've already processed this or a newer input
	if input.Sequence <= player.LastInput {
		return false
	}

	// Add to input queue
	player.InputQueue = append(player.InputQueue, input)
	player.LastSeen = time.Now()
	return true
}

// ProcessInputs processes all queued inputs for all players.
// Call this once per tick.
func (s *State) ProcessInputs() {
	s.mu.Lock()
	defer s.mu.Unlock()

	dt := 1.0 / float32(s.config.TickRate)

	for _, player := range s.players {
		// Sort and process inputs by sequence
		for _, input := range player.InputQueue {
			// Apply movement
			player.Velocity.X = input.Movement.X * s.config.PlayerSpeed
			player.Velocity.Y = input.Movement.Y * s.config.PlayerSpeed

			// Update position
			player.Position.X += player.Velocity.X * dt
			player.Position.Y += player.Velocity.Y * dt

			// Clamp to world bounds
			if player.Position.X < 0 {
				player.Position.X = 0
			}
			if player.Position.X > s.config.WorldWidth {
				player.Position.X = s.config.WorldWidth
			}
			if player.Position.Y < 0 {
				player.Position.Y = 0
			}
			if player.Position.Y > s.config.WorldHeight {
				player.Position.Y = s.config.WorldHeight
			}

			player.LastInput = input.Sequence
		}

		// Clear processed inputs
		if len(player.InputQueue) > 0 {
			player.InputQueue = player.InputQueue[:0]
		}
	}
}

// UpdateLastSeen updates the last seen time for a player.
func (s *State) UpdateLastSeen(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if player, ok := s.players[playerID]; ok {
		player.LastSeen = time.Now()
	}
}