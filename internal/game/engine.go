package game

import (
	"log"
	"sync"
	"time"

	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
)

// Broadcaster sends messages to players.
type Broadcaster interface {
	Broadcast(msg *gamepb.Message, excludeID string) error
	SendTo(addr string, msg *gamepb.Message) error
}

// Engine runs the game tick loop.
type Engine struct {
	state        *State
	config       Config
	broadcaster  Broadcaster
	tickRate     time.Duration
	running      bool
	stopCh       chan struct{}
	wg           sync.WaitGroup
	deltaTracker *DeltaTracker
}

// NewEngine creates a new game engine.
func NewEngine(config Config, broadcaster Broadcaster) *Engine {
	return &Engine{
		state:        NewState(config),
		config:       config,
		broadcaster:  broadcaster,
		tickRate:     time.Second / time.Duration(config.TickRate),
		stopCh:       make(chan struct{}),
		deltaTracker: NewDeltaTracker(),
	}
}

// Start begins the tick loop.
func (e *Engine) Start() {
	if e.running {
		return
	}
	e.running = true
	e.wg.Add(1)
	go e.tickLoop()
	log.Printf("🎮 Engine started: %d Hz (%v tick interval)", e.config.TickRate, e.tickRate)
}

// Stop stops the tick loop.
func (e *Engine) Stop() {
	if !e.running {
		return
	}
	e.running = false
	close(e.stopCh)
	e.wg.Wait()
	log.Println("🛑 Engine stopped")
}

// tickLoop runs at the configured tick rate.
func (e *Engine) tickLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.tickRate)
	defer ticker.Stop()

	lastBroadcast := time.Now()
	broadcastInterval := time.Second / 20 // 20 Hz state updates

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.tick()
		}

		// Broadcast state periodically
		if time.Since(lastBroadcast) >= broadcastInterval {
			e.broadcastState()
			lastBroadcast = time.Now()
		}
	}
}

// tick processes one game tick.
func (e *Engine) tick() {
	tick := e.state.Tick()

	// Process all queued inputs
	e.state.ProcessInputs()

	// Future: Process AI, physics, collisions, etc.

	_ = tick // Tick is tracked in state
}

// broadcastState sends state updates to all players using delta compression.
func (e *Engine) broadcastState() {
	players := e.state.AllPlayers()
	if len(players) == 0 {
		return
	}

	// Compute delta
	changed, removed := e.deltaTracker.ComputeDelta(players, false)

	// Skip if nothing changed
	if len(changed) == 0 && len(removed) == 0 {
		return
	}

	// Build delta message
	delta := &gamepb.GameStateDelta{
		Tick:           e.state.CurrentTick(),
		Timestamp:      uint64(time.Now().UnixMilli()),
		ChangedPlayers: make([]*gamepb.PlayerState, 0, len(changed)),
		RemovedPlayers: removed,
	}

	for _, p := range changed {
		delta.ChangedPlayers = append(delta.ChangedPlayers, p.ToProto())
	}

	msg := &gamepb.Message{
		Payload: &gamepb.Message_StateDelta{
			StateDelta: delta,
		},
	}

	// Broadcast to all
	if e.broadcaster != nil {
		e.broadcaster.Broadcast(msg, "")
	}
}

// State returns the game state for external access.
func (e *Engine) State() *State {
	return e.state
}

// CurrentTick returns the current game tick.
func (e *Engine) CurrentTick() uint64 {
	return e.state.CurrentTick()
}

// AddPlayer adds a player to the game.
func (e *Engine) AddPlayer(name, addr string) *Player {
	player := e.state.AddPlayer(name, addr)
	if player == nil {
		return nil
	}

	// Notify others of join
	if e.broadcaster != nil {
		msg := &gamepb.Message{
			Payload: &gamepb.Message_PlayerJoin{
				PlayerJoin: &gamepb.PlayerJoin{
					Player: &gamepb.PlayerState{
						PlayerId: player.ID,
						Position: &gamepb.Vec2{X: player.Position.X, Y: player.Position.Y},
						Velocity: &gamepb.Vec2{X: player.Velocity.X, Y: player.Velocity.Y},
					},
				},
			},
		}
		e.broadcaster.Broadcast(msg, player.ID)
	}

	log.Printf("✅ Player joined: %s (%s) at (%.1f, %.1f)", name, player.ID, player.Position.X, player.Position.Y)
	return player
}

// RemovePlayer removes a player from the game.
func (e *Engine) RemovePlayer(id string) {
	player := e.state.GetPlayer(id)
	if player == nil {
		return
	}

	e.state.RemovePlayer(id)

	// Clear from delta tracker
	delete(e.deltaTracker.lastStates, id)

	// Notify others of leave
	if e.broadcaster != nil {
		msg := &gamepb.Message{
			Payload: &gamepb.Message_PlayerLeave{
				PlayerLeave: &gamepb.PlayerLeave{
					PlayerId: id,
					Reason:   "disconnect",
				},
			},
		}
		e.broadcaster.Broadcast(msg, "")
	}

	log.Printf("❎ Player left: %s (%s)", player.Name, id)
}

// ApplyInput applies player input.
func (e *Engine) ApplyInput(playerID string, input Input) {
	e.state.ApplyInput(playerID, input)
}

// GetPlayerByAddr finds a player by their UDP address.
func (e *Engine) GetPlayerByAddr(addr string) *Player {
	return e.state.GetPlayerByAddr(addr)
}

// PlayerCount returns current player count.
func (e *Engine) PlayerCount() int {
	return e.state.PlayerCount()
}

// SendFullSnapshot sends a complete state snapshot to a specific player.
// Use when a player first joins.
func (e *Engine) SendFullSnapshot(addr string) {
	players := e.state.AllPlayers()

	snapshot := &gamepb.GameStateSnapshot{
		Tick:      e.state.CurrentTick(),
		Timestamp: uint64(time.Now().UnixMilli()),
		Players:   make([]*gamepb.PlayerState, 0, len(players)),
	}

	for _, p := range players {
		snapshot.Players = append(snapshot.Players, &gamepb.PlayerState{
			PlayerId: p.ID,
			Position: &gamepb.Vec2{X: p.Position.X, Y: p.Position.Y},
			Velocity: &gamepb.Vec2{X: p.Velocity.X, Y: p.Velocity.Y},
			Rotation: 0,
			Timestamp: uint64(p.LastSeen.UnixMilli()),
		})
	}

	msg := &gamepb.Message{
		Payload: &gamepb.Message_StateSnapshot{
			StateSnapshot: snapshot,
		},
	}

	if e.broadcaster != nil {
		e.broadcaster.SendTo(addr, msg)
	}
}