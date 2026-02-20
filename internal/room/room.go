package room

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Config for room settings
type Config struct {
	MaxPlayers    int           `json:"max_players"`
	RoomTTL       time.Duration `json:"room_ttl"`        // Time before empty room expires
	CleanupPeriod time.Duration `json:"cleanup_period"` // How often to check for expired rooms
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxPlayers:    8,
		RoomTTL:       5 * time.Minute,
		CleanupPeriod: 30 * time.Second,
	}
}

// Player in a room
type Player struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	JoinedAt time.Time `json:"joined_at"`
	IsHost   bool      `json:"is_host"`
}

// Room represents a game room
type Room struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	Players   map[string]Player `json:"players"`
	HostID    string            `json:"host_id"`
	MaxPlayer int               `json:"max_players"`

	// Internal
	lastActivity time.Time
	config       Config
	mu           sync.RWMutex
}

// Registry manages all rooms
type Registry struct {
	rooms  map[string]*Room
	config Config
	mu     sync.RWMutex

	// Callbacks
	onRoomExpired func(*Room)
}

// NewRegistry creates a new room registry
func NewRegistry(config Config) *Registry {
	r := &Registry{
		rooms:  make(map[string]*Room),
		config: config,
	}
	go r.cleanupLoop()
	return r
}

// generateID creates a short, shareable room ID
func generateID() string {
	b := make([]byte, 3) // 6 hex chars
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Create creates a new room and returns it
func (r *Registry) Create() *Room {
	r.mu.Lock()
	defer r.mu.Unlock()

	room := &Room{
		ID:           generateID(),
		CreatedAt:    time.Now(),
		Players:      make(map[string]Player),
		MaxPlayer:    r.config.MaxPlayers,
		lastActivity: time.Now(),
		config:       r.config,
	}
	r.rooms[room.ID] = room
	return room
}

// Get retrieves a room by ID
func (r *Registry) Get(id string) *Room {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rooms[id]
}

// Join adds a player to a room. Returns the room, player, or error.
func (r *Registry) Join(roomID, playerID, playerName string) (*Room, *Player, error) {
	r.mu.Lock()
	room, exists := r.rooms[roomID]
	if !exists {
		r.mu.Unlock()
		return nil, nil, ErrRoomNotFound
	}
	r.mu.Unlock()

	player, err := room.Join(playerID, playerName)
	if err != nil {
		return nil, nil, err
	}
	return room, player, nil
}

// Join adds a player to the room
func (room *Room) Join(playerID, playerName string) (*Player, error) {
	room.mu.Lock()
	defer room.mu.Unlock()

	if len(room.Players) >= room.MaxPlayer {
		return nil, ErrRoomFull
	}

	// If player already in room, just return them
	if p, exists := room.Players[playerID]; exists {
		return &p, nil
	}

	// First player is host
	isHost := len(room.Players) == 0
	if isHost {
		room.HostID = playerID
	}

	player := Player{
		ID:       playerID,
		Name:     playerName,
		JoinedAt: time.Now(),
		IsHost:   isHost,
	}
	room.Players[playerID] = player
	room.lastActivity = time.Now()

	return &player, nil
}

// Leave removes a player from the room
func (room *Room) Leave(playerID string) {
	room.mu.Lock()
	defer room.mu.Unlock()

	delete(room.Players, playerID)
	room.lastActivity = time.Now()

	// If host left, assign new host
	if playerID == room.HostID && len(room.Players) > 0 {
		// Pick first remaining player as new host
		for id, p := range room.Players {
			p.IsHost = true
			room.Players[id] = p
			room.HostID = id
			break
		}
	}
}

// PlayerCount returns the number of players in the room
func (room *Room) PlayerCount() int {
	room.mu.RLock()
	defer room.mu.RUnlock()
	return len(room.Players)
}

// PlayerIDs returns a list of player IDs in the room
func (room *Room) PlayerIDs() []string {
	room.mu.RLock()
	defer room.mu.RUnlock()
	ids := make([]string, 0, len(room.Players))
	for id := range room.Players {
		ids = append(ids, id)
	}
	return ids
}

// IsEmpty returns true if the room has no players
func (room *Room) IsEmpty() bool {
	return room.PlayerCount() == 0
}

// IsExpired returns true if the room has been empty longer than TTL
func (room *Room) IsExpired() bool {
	room.mu.RLock()
	defer room.mu.RUnlock()

	if len(room.Players) > 0 {
		return false
	}
	return time.Since(room.lastActivity) > room.config.RoomTTL
}

// Delete removes a room from the registry
func (r *Registry) Delete(roomID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rooms, roomID)
}

// OnRoomExpired sets a callback for when a room expires
func (r *Registry) OnRoomExpired(callback func(*Room)) {
	r.onRoomExpired = callback
}

// cleanupLoop periodically removes expired rooms
func (r *Registry) cleanupLoop() {
	ticker := time.NewTicker(r.config.CleanupPeriod)
	defer ticker.Stop()

	for range ticker.C {
		r.mu.Lock()
		for id, room := range r.rooms {
			if room.IsExpired() {
				if r.onRoomExpired != nil {
					go r.onRoomExpired(room)
				}
				delete(r.rooms, id)
			}
		}
		r.mu.Unlock()
	}
}

// AllRooms returns all active rooms (for debugging/admin)
func (r *Registry) AllRooms() []*Room {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rooms := make([]*Room, 0, len(r.rooms))
	for _, room := range r.rooms {
		rooms = append(rooms, room)
	}
	return rooms
}

// Count returns the total number of rooms
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.rooms)
}