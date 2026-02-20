package game

import (
	"testing"
)

func TestNewState(t *testing.T) {
	config := DefaultConfig()
	state := NewState(config)

	if state.PlayerCount() != 0 {
		t.Errorf("expected 0 players, got %d", state.PlayerCount())
	}
}

func TestAddPlayer(t *testing.T) {
	state := NewState(DefaultConfig())

	p := state.AddPlayer("TestPlayer", "127.0.0.1:1234")
	if p == nil {
		t.Fatal("expected player, got nil")
	}

	if p.Name != "TestPlayer" {
		t.Errorf("expected name TestPlayer, got %s", p.Name)
	}

	if p.Addr != "127.0.0.1:1234" {
		t.Errorf("expected addr 127.0.0.1:1234, got %s", p.Addr)
	}

	// Should spawn at center
	config := DefaultConfig()
	if p.Position.X != config.WorldWidth/2 || p.Position.Y != config.WorldHeight/2 {
		t.Errorf("expected center position, got (%.1f, %.1f)", p.Position.X, p.Position.Y)
	}

	if state.PlayerCount() != 1 {
		t.Errorf("expected 1 player, got %d", state.PlayerCount())
	}
}

func TestAddPlayerMaxPlayers(t *testing.T) {
	config := Config{
		TickRate:    60,
		MaxPlayers:  2,
		PlayerSpeed: 100,
		WorldWidth:  1000,
		WorldHeight: 1000,
	}
	state := NewState(config)

	p1 := state.AddPlayer("P1", "127.0.0.1:1234")
	p2 := state.AddPlayer("P2", "127.0.0.1:1235")
	p3 := state.AddPlayer("P3", "127.0.0.1:1236") // Should fail

	if p1 == nil || p2 == nil {
		t.Error("expected first two players to be added")
	}

	if p3 != nil {
		t.Error("expected third player to fail (max players)")
	}

	if state.PlayerCount() != 2 {
		t.Errorf("expected 2 players, got %d", state.PlayerCount())
	}
}

func TestRemovePlayer(t *testing.T) {
	state := NewState(DefaultConfig())

	p := state.AddPlayer("TestPlayer", "127.0.0.1:1234")
	if p == nil {
		t.Fatal("expected player")
	}

	state.RemovePlayer(p.ID)

	if state.PlayerCount() != 0 {
		t.Errorf("expected 0 players after remove, got %d", state.PlayerCount())
	}

	if state.GetPlayer(p.ID) != nil {
		t.Error("expected player to be nil after remove")
	}
}

func TestApplyInput(t *testing.T) {
	config := Config{
		TickRate:    60,
		MaxPlayers:  100,
		PlayerSpeed: 60, // 60 units/sec = 1 unit/tick at 60Hz
		WorldWidth:  1000,
		WorldHeight: 1000,
	}
	state := NewState(config)

	p := state.AddPlayer("TestPlayer", "127.0.0.1:1234")
	initialX := p.Position.X

	// Apply input to move right
	state.ApplyInput(p.ID, Input{
		Sequence: 1,
		Movement: Vec2{X: 1, Y: 0},
	})

	// After one tick, should have moved 1 unit right
	if p.Position.X <= initialX {
		t.Errorf("expected position to increase, got %.2f -> %.2f", initialX, p.Position.X)
	}

	// Velocity should be set
	if p.Velocity.X != config.PlayerSpeed {
		t.Errorf("expected velocity %.2f, got %.2f", config.PlayerSpeed, p.Velocity.X)
	}

	// LastInput should be updated
	if p.LastInput != 1 {
		t.Errorf("expected LastInput=1, got %d", p.LastInput)
	}
}

func TestApplyInputWorldBounds(t *testing.T) {
	config := Config{
		TickRate:    60,
		MaxPlayers:  100,
		PlayerSpeed: 1000, // Very fast
		WorldWidth:  100,
		WorldHeight: 100,
	}
	state := NewState(config)

	// Place player at edge
	p := state.AddPlayer("TestPlayer", "127.0.0.1:1234")
	p.Position.X = 99 // Near right edge

	// Try to move past boundary
	for i := 0; i < 10; i++ {
		state.ApplyInput(p.ID, Input{
			Sequence: uint64(i + 1),
			Movement: Vec2{X: 1, Y: 0},
		})
	}

	if p.Position.X > config.WorldWidth {
		t.Errorf("player escaped world bounds: X=%.2f > %.2f", p.Position.X, config.WorldWidth)
	}
}

func TestApplyInputIgnoresOldSequence(t *testing.T) {
	state := NewState(DefaultConfig())

	p := state.AddPlayer("TestPlayer", "127.0.0.1:1234")
	p.LastInput = 5

	// Try to apply old input
	state.ApplyInput(p.ID, Input{
		Sequence: 3, // Less than current
		Movement: Vec2{X: 1, Y: 0},
	})

	if p.LastInput != 5 {
		t.Error("old input should be ignored")
	}
}

func TestTick(t *testing.T) {
	state := NewState(DefaultConfig())

	tick := state.Tick()
	if tick != 1 {
		t.Errorf("expected tick 1, got %d", tick)
	}

	tick = state.Tick()
	if tick != 2 {
		t.Errorf("expected tick 2, got %d", tick)
	}
}

func TestGetPlayerByAddr(t *testing.T) {
	state := NewState(DefaultConfig())

	p := state.AddPlayer("TestPlayer", "127.0.0.1:1234")

	found := state.GetPlayerByAddr("127.0.0.1:1234")
	if found == nil {
		t.Error("expected to find player by addr")
	}

	if found.ID != p.ID {
		t.Errorf("found wrong player: %s vs %s", found.ID, p.ID)
	}

	notFound := state.GetPlayerByAddr("127.0.0.1:9999")
	if notFound != nil {
		t.Error("expected nil for unknown addr")
	}
}

func BenchmarkApplyInput(b *testing.B) {
	state := NewState(DefaultConfig())
	p := state.AddPlayer("TestPlayer", "127.0.0.1:1234")

	input := Input{
		Movement: Vec2{X: 0.5, Y: -0.5},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input.Sequence = uint64(i + 1)
		state.ApplyInput(p.ID, input)
	}
}