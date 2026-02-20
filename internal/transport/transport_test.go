package transport

import (
	"testing"
)

func TestMockTransport_SendMessage(t *testing.T) {
	mock := NewMockTransport()

	var received []byte
	mock.OnMessage(func(addr string, data []byte, reliable bool) {
		received = data
	})

	// Simulate receiving a message
	mock.SimulateMessage("127.0.0.1:1234", []byte("hello"), false)

	if string(received) != "hello" {
		t.Errorf("expected 'hello', got '%s'", received)
	}
}

func TestMockTransport_SendUnreliable(t *testing.T) {
	mock := NewMockTransport()
	_ = mock.Listen(":9000")

	err := mock.SendUnreliable("127.0.0.1:1234", []byte("ping"))
	if err != nil {
		t.Fatalf("SendUnreliable failed: %v", err)
	}

	sent := mock.SentMessages()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sent))
	}

	if string(sent[0].Data) != "ping" {
		t.Errorf("expected 'ping', got '%s'", sent[0].Data)
	}
}

func TestMockTransport_ConnectDisconnect(t *testing.T) {
	mock := NewMockTransport()

	var connected, disconnected string
	mock.OnConnect(func(addr string) {
		connected = addr
	})
	mock.OnDisconnect(func(addr string) {
		disconnected = addr
	})

	mock.SimulateConnect("127.0.0.1:1234")
	if connected != "127.0.0.1:1234" {
		t.Errorf("expected connect callback, got '%s'", connected)
	}

	mock.SimulateDisconnect("127.0.0.1:1234")
	if disconnected != "127.0.0.1:1234" {
		t.Errorf("expected disconnect callback, got '%s'", disconnected)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxMessageSize != 1400 {
		t.Errorf("expected MaxMessageSize 1400, got %d", cfg.MaxMessageSize)
	}
}