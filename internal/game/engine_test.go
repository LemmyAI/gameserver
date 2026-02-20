package game

import (
	"testing"
	"time"

	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
)

// mockBroadcaster captures broadcast messages for testing
type mockBroadcaster struct {
	messages []*gamepb.Message
	sent     []struct {
		addr string
		msg  *gamepb.Message
	}
}

func (m *mockBroadcaster) Broadcast(msg *gamepb.Message, excludeID string) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockBroadcaster) SendTo(addr string, msg *gamepb.Message) error {
	m.sent = append(m.sent, struct {
		addr string
		msg  *gamepb.Message
	}{addr, msg})
	return nil
}

func TestEngineStartStop(t *testing.T) {
	broadcaster := &mockBroadcaster{}
	config := DefaultConfig()
	engine := NewEngine(config, broadcaster)

	engine.Start()
	time.Sleep(20 * time.Millisecond) // Let it tick a few times
	engine.Stop()

	tick := engine.CurrentTick()
	if tick < 1 {
		t.Errorf("expected at least 1 tick, got %d", tick)
	}
}

func TestEngineAddRemovePlayer(t *testing.T) {
	broadcaster := &mockBroadcaster{}
	engine := NewEngine(DefaultConfig(), broadcaster)

	player := engine.AddPlayer("TestPlayer", "127.0.0.1:1234")
	if player == nil {
		t.Fatal("expected player to be added")
	}

	if engine.PlayerCount() != 1 {
		t.Errorf("expected 1 player, got %d", engine.PlayerCount())
	}

	// Should broadcast PlayerJoin
	if len(broadcaster.messages) != 1 {
		t.Fatalf("expected 1 broadcast message, got %d", len(broadcaster.messages))
	}

	joinMsg := broadcaster.messages[0].GetPlayerJoin()
	if joinMsg == nil {
		t.Fatal("expected PlayerJoin message")
	}

	if joinMsg.Player.PlayerId != player.ID {
		t.Errorf("expected player ID %s, got %s", player.ID, joinMsg.Player.PlayerId)
	}

	// Remove player
	engine.RemovePlayer(player.ID)

	if engine.PlayerCount() != 0 {
		t.Errorf("expected 0 players after remove, got %d", engine.PlayerCount())
	}

	// Should broadcast PlayerLeave
	if len(broadcaster.messages) != 2 {
		t.Fatalf("expected 2 broadcast messages, got %d", len(broadcaster.messages))
	}

	leaveMsg := broadcaster.messages[1].GetPlayerLeave()
	if leaveMsg == nil {
		t.Fatal("expected PlayerLeave message")
	}
}

func TestEngineInputProcessing(t *testing.T) {
	broadcaster := &mockBroadcaster{}
	config := Config{
		TickRate:    60,
		MaxPlayers:  100,
		PlayerSpeed: 60, // 60 units/sec = 1 unit/tick at 60Hz
		WorldWidth:  1000,
		WorldHeight: 1000,
	}
	engine := NewEngine(config, broadcaster)

	player := engine.AddPlayer("TestPlayer", "127.0.0.1:1234")
	initialX := player.Position.X

	// Queue some inputs
	engine.ApplyInput(player.ID, Input{
		Sequence: 1,
		Movement:  Vec2{X: 1, Y: 0},
	})
	engine.ApplyInput(player.ID, Input{
		Sequence: 2,
		Movement:  Vec2{X: 1, Y: 0},
	})

	// Position shouldn't change until tick
	if player.Position.X != initialX {
		t.Error("position changed before tick")
	}

	// Process inputs
	engine.State().ProcessInputs()

	// Position should now be updated
	if player.Position.X <= initialX {
		t.Errorf("expected position to increase after tick, got %.2f", player.Position.X)
	}

	// Last processed input should be 2
	if player.LastInput != 2 {
		t.Errorf("expected LastInput=2, got %d", player.LastInput)
	}
}

func TestDeltaTracker(t *testing.T) {
	tracker := NewDeltaTracker()

	players := []*Player{
		{
			ID:       "p1",
			Position: Vec2{X: 100, Y: 100},
			Velocity: Vec2{X: 0, Y: 0},
		},
		{
			ID:       "p2",
			Position: Vec2{X: 200, Y: 200},
			Velocity: Vec2{X: 0, Y: 0},
		},
	}

	// First call with fullSync should return all
	changed, removed := tracker.ComputeDelta(players, true)
	if len(changed) != 2 {
		t.Errorf("expected 2 changed players, got %d", len(changed))
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removed players, got %d", len(removed))
	}

	// No change, no delta
	changed, removed = tracker.ComputeDelta(players, false)
	if len(changed) != 0 {
		t.Errorf("expected 0 changed (no movement), got %d", len(changed))
	}

	// Move player 1
	players[0].Position.X = 150
	changed, removed = tracker.ComputeDelta(players, false)
	if len(changed) != 1 {
		t.Errorf("expected 1 changed (p1 moved), got %d", len(changed))
	}
	if changed[0].ID != "p1" {
		t.Errorf("expected p1 to be changed, got %s", changed[0].ID)
	}

	// Remove player 2
	players = players[:1]
	changed, removed = tracker.ComputeDelta(players, false)
	if len(removed) != 1 {
		t.Errorf("expected 1 removed player, got %d", len(removed))
	}
	if removed[0] != "p2" {
		t.Errorf("expected p2 to be removed, got %s", removed[0])
	}
}

func TestEngineBroadcastState(t *testing.T) {
	broadcaster := &mockBroadcaster{}
	engine := NewEngine(DefaultConfig(), broadcaster)

	// Add two players
	engine.AddPlayer("P1", "127.0.0.1:1234")
	engine.AddPlayer("P2", "127.0.0.1:1235")

	// Clear join messages
	broadcaster.messages = nil

	// First broadcast will be delta with both players (they're new to tracker)
	// Move a player significantly
	p1 := engine.State().GetPlayerByAddr("127.0.0.1:1234")
	p1.Position.X = 600 // Significant movement (> 0.1 epsilon)

	// Broadcast state - first broadcast will include both players
	engine.broadcastState()

	// Should have sent a delta
	if len(broadcaster.messages) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(broadcaster.messages))
	}

	delta := broadcaster.messages[0].GetStateDelta()
	if delta == nil {
		t.Fatal("expected StateDelta message")
	}

	// First broadcast sends all players (they're tracked now)
	// Clear and move again
	broadcaster.messages = nil
	p1.Position.X = 700 // Another significant move

	engine.broadcastState()

	delta = broadcaster.messages[0].GetStateDelta()
	if delta == nil {
		t.Fatal("expected StateDelta message")
	}

	// Should have only 1 changed player (p1 moved again)
	if len(delta.ChangedPlayers) != 1 {
		t.Errorf("expected 1 changed player (p1 moved), got %d", len(delta.ChangedPlayers))
	}
}

func TestEngineSendFullSnapshot(t *testing.T) {
	broadcaster := &mockBroadcaster{}
	engine := NewEngine(DefaultConfig(), broadcaster)

	engine.AddPlayer("P1", "127.0.0.1:1234")
	engine.AddPlayer("P2", "127.0.0.1:1235")

	// Send full snapshot to a specific address
	engine.SendFullSnapshot("127.0.0.1:9999")

	if len(broadcaster.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(broadcaster.sent))
	}

	snapshot := broadcaster.sent[0].msg.GetStateSnapshot()
	if snapshot == nil {
		t.Fatal("expected StateSnapshot message")
	}

	if len(snapshot.Players) != 2 {
		t.Errorf("expected 2 players in snapshot, got %d", len(snapshot.Players))
	}
}

// Benchmarks

func BenchmarkEngineTick(b *testing.B) {
	broadcaster := &mockBroadcaster{}
	engine := NewEngine(DefaultConfig(), broadcaster)

	// Add 100 players
	for i := 0; i < 100; i++ {
		engine.AddPlayer("P", "127.0.0.1:1234")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.State().Tick()
		engine.State().ProcessInputs()
	}
}

func BenchmarkDeltaCompute(b *testing.B) {
	tracker := NewDeltaTracker()

	// Create 100 players
	players := make([]*Player, 100)
	for i := range players {
		players[i] = &Player{
			ID:       string(rune('A' + i%26)),
			Position: Vec2{X: float32(i * 10), Y: float32(i * 10)},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Move some players
		players[i%100].Position.X += 0.5
		tracker.ComputeDelta(players, false)
	}
}