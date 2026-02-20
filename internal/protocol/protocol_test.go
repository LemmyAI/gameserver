package protocol

import (
	"testing"

	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
)

func TestEncodeDecodeClientHello(t *testing.T) {
	original := NewClientHello("player-123", "TestPlayer", "1.0.0")

	data, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	hello := decoded.GetClientHello()
	if hello == nil {
		t.Fatal("Expected ClientHello payload")
	}
	if hello.PlayerId != "player-123" {
		t.Errorf("expected player-123, got %s", hello.PlayerId)
	}
	if hello.PlayerName != "TestPlayer" {
		t.Errorf("expected TestPlayer, got %s", hello.PlayerName)
	}
	if hello.Version != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", hello.Version)
	}
}

func TestEncodeDecodePlayerInput(t *testing.T) {
	original := NewPlayerInput(1, 1708444800000, 0.5, -1.0, true, false, true)

	data, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	input := decoded.GetPlayerInput()
	if input == nil {
		t.Fatal("Expected PlayerInput payload")
	}
	if input.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", input.Sequence)
	}
	if input.Movement.X != 0.5 || input.Movement.Y != -1.0 {
		t.Errorf("expected movement (0.5, -1.0), got (%.2f, %.2f)", input.Movement.X, input.Movement.Y)
	}
	if !input.Jump {
		t.Error("expected jump=true")
	}
}

func TestMessageTypeName(t *testing.T) {
	tests := []struct {
		msg      *gamepb.Message
		expected string
	}{
		{NewClientHello("x", "y", "z"), "ClientHello"},
		{NewServerWelcome("x", 60, 0), "ServerWelcome"},
		{NewPlayerInput(0, 0, 0, 0, false, false, false), "PlayerInput"},
	}

	for _, tt := range tests {
		got := MessageTypeName(tt.msg)
		if got != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, got)
		}
	}
}

func BenchmarkEncodePlayerInput(b *testing.B) {
	msg := NewPlayerInput(1, 1708444800000, 0.5, -1.0, true, false, false)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Encode(msg)
	}
}

func BenchmarkDecodePlayerInput(b *testing.B) {
	msg := NewPlayerInput(1, 1708444800000, 0.5, -1.0, true, false, false)
	data, _ := Encode(msg)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Decode(data)
	}
}