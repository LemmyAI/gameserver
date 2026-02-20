package game

import (
	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
)

// DeltaTracker tracks player state changes for delta compression.
type DeltaTracker struct {
	lastStates map[string]*playerSnapshot
}

type playerSnapshot struct {
	x, y     float32
	vx, vy   float32
	rotation float32
}

// NewDeltaTracker creates a new delta tracker.
func NewDeltaTracker() *DeltaTracker {
	return &DeltaTracker{
		lastStates: make(map[string]*playerSnapshot),
	}
}

// ComputeDelta returns only changed players since last call.
// If fullSync is true, returns all players and clears history.
func (d *DeltaTracker) ComputeDelta(players []*Player, fullSync bool) (changed []*PlayerState, removed []string) {
	changed = make([]*PlayerState, 0)
	removed = make([]string, 0)

	// Find removed players
	currentIDs := make(map[string]bool)
	for _, p := range players {
		currentIDs[p.ID] = true
	}

	for id := range d.lastStates {
		if !currentIDs[id] {
			removed = append(removed, id)
			delete(d.lastStates, id)
		}
	}

	// Find changed players
	for _, p := range players {
		snapshot := &playerSnapshot{
			x:        p.Position.X,
			y:        p.Position.Y,
			vx:       p.Velocity.X,
			vy:       p.Velocity.Y,
			rotation: 0,
		}

		last, exists := d.lastStates[p.ID]

		if fullSync || !exists || d.hasChanged(last, snapshot) {
			changed = append(changed, &PlayerState{
				ID:        p.ID,
				Position:  p.Position,
				Velocity:  p.Velocity,
				Rotation:  0,
				Timestamp: uint64(p.LastSeen.UnixMilli()),
			})
			d.lastStates[p.ID] = snapshot
		}
	}

	return changed, removed
}

// hasChanged checks if player state has meaningfully changed.
// Uses epsilon to avoid sending tiny movements.
func (d *DeltaTracker) hasChanged(old, new *playerSnapshot) bool {
	const epsilon = 0.1 // 0.1 units threshold

	if abs(new.x-old.x) > epsilon || abs(new.y-old.y) > epsilon {
		return true
	}
	if abs(new.vx-old.vx) > epsilon || abs(new.vy-old.vy) > epsilon {
		return true
	}
	return false
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// Clear resets all tracked state.
func (d *DeltaTracker) Clear() {
	d.lastStates = make(map[string]*playerSnapshot)
}

// PlayerState is a snapshot for delta messages.
type PlayerState struct {
	ID        string
	Position  Vec2
	Velocity  Vec2
	Rotation  float32
	Timestamp uint64
}

// ToProto converts PlayerState to protobuf.
func (p *PlayerState) ToProto() *gamepb.PlayerState {
	return &gamepb.PlayerState{
		PlayerId: p.ID,
		Position: &gamepb.Vec2{X: p.Position.X, Y: p.Position.Y},
		Velocity: &gamepb.Vec2{X: p.Velocity.X, Y: p.Velocity.Y},
		Rotation: p.Rotation,
		Timestamp: p.Timestamp,
	}
}