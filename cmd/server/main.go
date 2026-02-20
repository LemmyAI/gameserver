// Command server is the main UDP game server.
// Phase 2: Game engine with tick loop and player state.
package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/LemmyAI/gameserver/internal/game"
	"github.com/LemmyAI/gameserver/internal/protocol"
	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
	"github.com/LemmyAI/gameserver/internal/transport"
)

// Server holds all server state.
type Server struct {
	transport   transport.Transport
	engine      *game.Engine
	broadcaster *game.TransportBroadcaster
	playerMap   map[string]string // addr -> playerID
	mu          sync.RWMutex
}

func main() {
	log.Println("🎮 GameServer starting...")

	// Create UDP transport
	t := transport.NewUDPTransport(transport.DefaultConfig())

	// Create server
	srv := &Server{
		transport: t,
		playerMap: make(map[string]string),
	}

	// Create game engine with broadcaster
	config := game.DefaultConfig()
	srv.broadcaster = game.NewTransportBroadcaster(nil, t.SendUnreliable)
	srv.engine = game.NewEngine(config, srv.broadcaster)
	srv.broadcaster.SetState(srv.engine.State())

	// Register transport handlers
	t.OnMessage(srv.handleMessage)
	t.OnConnect(srv.handleConnect)
	t.OnDisconnect(srv.handleDisconnect)

	// Start game engine
	srv.engine.Start()

	// Start HTTP health server (for Render)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	go startHTTPServer(port, srv)

	// Start UDP listener
	udpPort := os.Getenv("UDP_PORT")
	if udpPort == "" {
		udpPort = ":9000"
	} else {
		udpPort = ":" + udpPort
	}
	log.Printf("🎧 Listening on UDP %s", udpPort)
	if err := t.Listen(udpPort); err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Printf("✅ Server ready!")
	log.Printf("   UDP: %s", udpPort)
	log.Printf("   HTTP: :%s", port)
	log.Printf("   Tick rate: %d Hz, World: %.0fx%.0f", config.TickRate, config.WorldWidth, config.WorldHeight)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("🛑 Shutting down...")
	srv.engine.Stop()
	if err := t.Close(); err != nil {
		log.Printf("Error closing: %v", err)
	}
	log.Println("👋 Bye!")
}

// startHTTPServer starts an HTTP server for health checks and metrics.
func startHTTPServer(port string, srv *Server) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"players": ` + itoa(srv.engine.PlayerCount()) + `}`))
	})

	log.Printf("🏥 HTTP server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("HTTP server error: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

// handleMessage processes incoming messages.
func (s *Server) handleMessage(addr string, data []byte, reliable bool) {
	// Decode message
	msg, err := protocol.Decode(data)
	if err != nil {
		log.Printf("⚠️  [%s] invalid protobuf: %v", addr, err)
		return
	}

	// Route by message type
	switch payload := msg.Payload.(type) {
	case *gamepb.Message_ClientHello:
		s.handleClientHello(addr, payload.ClientHello)
	case *gamepb.Message_PlayerInput:
		s.handlePlayerInput(addr, payload.PlayerInput)
	default:
		log.Printf("❓ [%s] unknown message type: %s", addr, protocol.MessageTypeName(msg))
	}
}

// handleConnect handles new connections (UDP doesn't really have these).
func (s *Server) handleConnect(addr string) {
	// UDP is connectionless - we handle "connect" via ClientHello
}

// handleDisconnect handles disconnections.
func (s *Server) handleDisconnect(addr string) {
	s.mu.Lock()
	playerID, ok := s.playerMap[addr]
	delete(s.playerMap, addr)
	s.mu.Unlock()

	if ok {
		s.engine.RemovePlayer(playerID)
	}
}

// handleClientHello handles new player connections.
func (s *Server) handleClientHello(addr string, hello *gamepb.ClientHello) {
	// Check if already connected
	s.mu.RLock()
	_, exists := s.playerMap[addr]
	s.mu.RUnlock()

	if exists {
		log.Printf("⚠️  [%s] already connected", addr)
		return
	}

	// Add player to game
	player := s.engine.AddPlayer(hello.PlayerName, addr)
	if player == nil {
		log.Printf("❌ [%s] server full", addr)
		return
	}

	// Track addr -> playerID mapping
	s.mu.Lock()
	s.playerMap[addr] = player.ID
	s.mu.Unlock()

	// Send welcome
	welcome := protocol.NewServerWelcome(
		player.ID,
		uint32(s.engine.State().Config().TickRate),
		uint64(time.Now().UnixMilli()),
	)

	if err := s.broadcaster.SendTo(addr, welcome); err != nil {
		log.Printf("❌ send welcome: %v", err)
		return
	}

	log.Printf("👋 [%s] Welcome sent to %s (id=%s)", addr, hello.PlayerName, player.ID)
}

// handlePlayerInput handles player input.
func (s *Server) handlePlayerInput(addr string, input *gamepb.PlayerInput) {
	s.mu.RLock()
	playerID, ok := s.playerMap[addr]
	s.mu.RUnlock()

	if !ok {
		return
	}

	// Apply input to game state
	s.engine.ApplyInput(playerID, game.Input{
		Sequence:  input.Sequence,
		Timestamp: input.Timestamp,
		Movement: game.Vec2{
			X: input.Movement.GetX(),
			Y: input.Movement.GetY(),
		},
		Jump:    input.Jump,
		Action1: input.GetAction_1(),
		Action2: input.GetAction_2(),
	})
}