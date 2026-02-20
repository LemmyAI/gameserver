// Command server is the main UDP game server.
// Phase 1: Echo server - receives messages and sends them back.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LemmyAI/gameserver/internal/transport"
)

func main() {
	log.Println("🎮 GameServer starting...")

	// Create UDP transport
	t := transport.NewUDPTransport(transport.DefaultConfig())

	// Register message handler - echo back
	t.OnMessage(func(addr string, data []byte, reliable bool) {
		log.Printf("📥 [%s] received %d bytes", addr, len(data))

		// Echo back
		err := t.SendUnreliable(addr, data)
		if err != nil {
			log.Printf("❌ send error: %v", err)
			return
		}
		log.Printf("📤 [%s] echoed %d bytes", addr, len(data))
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

	log.Printf("✅ Server ready! Test with: echo 'hello' | nc -u localhost 9000")

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