// Command server is the main UDP game server.
// Phase 1: Proto echo server - receives protobuf messages, parses and responds.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LemmyAI/gameserver/internal/protocol"
	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
	"github.com/LemmyAI/gameserver/internal/transport"
)

func main() {
	log.Println("🎮 GameServer starting...")

	// Create UDP transport
	t := transport.NewUDPTransport(transport.DefaultConfig())

	// Register message handler
	t.OnMessage(func(addr string, data []byte, reliable bool) {
		// Try to decode as protobuf
		msg, err := protocol.Decode(data)
		if err != nil {
			log.Printf("⚠️  [%s] invalid protobuf: %v", addr, err)
			// Echo raw bytes for backward compatibility
			t.SendUnreliable(addr, data)
			return
		}

		msgType := protocol.MessageTypeName(msg)
		log.Printf("📥 [%s] %s (%d bytes)", addr, msgType, len(data))

		// Handle different message types
		switch payload := msg.Payload.(type) {
		case *gamepb.Message_ClientHello:
			handleClientHello(t, addr, payload.ClientHello)
		case *gamepb.Message_PlayerInput:
			handlePlayerInput(t, addr, payload.PlayerInput)
		default:
			// Echo back unknown messages
			t.SendUnreliable(addr, data)
		}
	})

	t.OnConnect(func(addr string) {
		log.Printf("✅ Client connected: %s", addr)
	})

	t.OnDisconnect(func(addr string) {
		log.Printf("❎ Client disconnected: %s", addr)
	})

	// Start listening
	addr := ":9000"
	log.Printf("🎧 Listening on UDP %s", addr)
	if err := t.Listen(addr); err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Printf("✅ Server ready!")
	log.Printf("   Test with: make test-client")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("🛑 Shutting down...")
	if err := t.Close(); err != nil {
		log.Printf("Error closing: %v", err)
	}
	log.Println("👋 Bye!")
}

func handleClientHello(t transport.Transport, addr string, hello *gamepb.ClientHello) {
	log.Printf("👋 Hello from %s (%s) version %s", hello.PlayerName, hello.PlayerId, hello.Version)

	// Send welcome
	welcome := protocol.NewServerWelcome(
		hello.PlayerId,
		60, // tick rate
		uint64(time.Now().UnixMilli()),
	)

	data, err := protocol.Encode(welcome)
	if err != nil {
		log.Printf("❌ encode welcome: %v", err)
		return
	}

	err = t.SendUnreliable(addr, data)
	if err != nil {
		log.Printf("❌ send welcome: %v", err)
		return
	}
	log.Printf("📤 [%s] ServerWelcome sent", addr)
}

func handlePlayerInput(t transport.Transport, addr string, input *gamepb.PlayerInput) {
	// For now, just acknowledge input
	log.Printf("🎮 [%s] Input seq=%d move=(%.2f,%.2f) jump=%v", 
		addr, input.Sequence, input.Movement.X, input.Movement.Y, input.Jump)
}