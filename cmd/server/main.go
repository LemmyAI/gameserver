// Command server is the main UDP game server.
// Phase 2: Game engine with tick loop and player state.
package main

import (
	"flag"
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
	playerMap   map[string]string // playerID -> addr (multiple players per addr OK)
	mu          sync.RWMutex
}

func main() {
	// Parse flags
	udpPort := flag.String("udp", "", "UDP port to listen on (default from env or 9000)")
	httpPort := flag.String("http", "", "HTTP port (default from env or 8000)")
	roomID := flag.String("room", "", "Room ID for logging")
	flag.Parse()

	log.Printf("🎮 GameServer starting... (room: %s)", *roomID)

	// Determine ports
	udpAddr := *udpPort
	if udpAddr == "" {
		udpAddr = os.Getenv("UDP_PORT")
	}
	if udpAddr == "" {
		udpAddr = "9000"
	}
	if udpAddr[0] != ':' {
		udpAddr = ":" + udpAddr
	}

	httpAddr := *httpPort
	if httpAddr == "" {
		httpAddr = os.Getenv("PORT")
	}
	if httpAddr == "" {
		httpAddr = "8000"
	}

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

	// Start HTTP health server
	go startHTTPServer(httpAddr, srv)

	// Start UDP listener
	log.Printf("🎧 Listening on UDP %s", udpAddr)
	if err := t.Listen(udpAddr); err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Printf("✅ Server ready!")
	log.Printf("   UDP: %s", udpAddr)
	log.Printf("   HTTP: :%s", httpAddr)
	log.Printf("   Tick rate: %d Hz, World: %.0fx%.0f", config.TickRate, config.WorldWidth, config.WorldHeight)
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
	// Find all players at this address and remove them
	s.mu.Lock()
	var toRemove []string
	for playerID, playerAddr := range s.playerMap {
		if playerAddr == addr {
			toRemove = append(toRemove, playerID)
		}
	}
	for _, playerID := range toRemove {
		delete(s.playerMap, playerID)
	}
	s.mu.Unlock()

	for _, playerID := range toRemove {
		s.engine.RemovePlayer(playerID)
	}
}

// handleClientHello handles new player connections.
func (s *Server) handleClientHello(addr string, hello *gamepb.ClientHello) {
	playerID := hello.PlayerId
	if playerID == "" {
		log.Printf("❌ [%s] empty player ID", addr)
		return
	}

	// Check if player ID already exists
	s.mu.RLock()
	_, exists := s.playerMap[playerID]
	s.mu.RUnlock()

	if exists {
		// Player already connected, just update address
		s.mu.Lock()
		s.playerMap[playerID] = addr
		s.mu.Unlock()
		return
	}

	// Add player to game
	player := s.engine.AddPlayerWithID(hello.PlayerName, playerID, addr)
	if player == nil {
		log.Printf("❌ [%s] server full or ID conflict", addr)
		return
	}

	// Track playerID -> addr mapping
	s.mu.Lock()
	s.playerMap[playerID] = addr
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

	log.Printf("👋 [%s] Welcome to %s (id=%s)", addr, hello.PlayerName, player.ID)
}

// handlePlayerInput handles player input.
func (s *Server) handlePlayerInput(addr string, input *gamepb.PlayerInput) {
	playerID := input.PlayerId
	if playerID == "" {
		return
	}

	// Verify this player exists
	s.mu.RLock()
	_, exists := s.playerMap[playerID]
	s.mu.RUnlock()

	if !exists {
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