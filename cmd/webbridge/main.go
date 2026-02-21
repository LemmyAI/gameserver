// WebBridge - WebSocket to UDP bridge with room support
// Each room spawns a separate game server process (true isolation)
// Native WebRTC via Pion for voice/video
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/google/uuid"

	"github.com/LemmyAI/gameserver/internal/protocol"
	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
	"github.com/LemmyAI/gameserver/internal/room"
	"github.com/LemmyAI/gameserver/internal/webrtc"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type BrowserClient struct {
	ws       *websocket.Conn
	playerID string
	name     string
	roomID   string
}

// GameRoom holds the game server process and connection for one room
type GameRoom struct {
	ID         string
	UDPConn    *net.UDPConn
	UDPAddr    *net.UDPAddr
	Process    *exec.Cmd
	State      map[string]*gamepb.PlayerState
	Mu         sync.RWMutex
	WebRTC     *webrtc.Manager // WebRTC manager for this room
}

type Bridge struct {
	clients      map[*websocket.Conn]*BrowserClient
	gameRooms    map[string]*GameRoom // roomID -> game room
	mu           sync.RWMutex
	rooms        *room.Registry
	basePort     int
}

func NewBridge() *Bridge {
	config := room.DefaultConfig()
	config.RoomTTL = 1 * time.Minute // Kill empty rooms after 1 minute

	bridge := &Bridge{
		clients:   make(map[*websocket.Conn]*BrowserClient),
		gameRooms: make(map[string]*GameRoom),
		rooms:     room.NewRegistry(config),
		basePort:  9100, // Game servers start at port 9100
	}

	// Register cleanup callback - kill game server when room expires
	bridge.rooms.OnRoomExpired(func(r *room.Room) {
		log.Printf("🗑️  Room %s expired (empty for 1 minute), stopping game server", r.ID)
		bridge.stopGameRoom(r.ID)
	})

	return bridge
}

// spawnGameServer creates a new game server process for a room
func (b *Bridge) spawnGameServer(roomID string) (*GameRoom, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if already exists
	if gr, exists := b.gameRooms[roomID]; exists {
		return gr, nil
	}

	// Calculate port (simple: 9100 + hash of roomID)
	port := b.basePort + (int(roomID[0]) % 1000)
	httpPort := port + 1000

	// Spawn server process
	cmd := exec.Command("./bin/server",
		"-udp", fmt.Sprintf("%d", port),
		"-http", fmt.Sprintf("%d", httpPort),
		"-room", roomID,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to spawn server: %w", err)
	}

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Resolve UDP address
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	// Create UDP connection to the new server
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("failed to dial server: %w", err)
	}

	gr := &GameRoom{
		ID:      roomID,
		UDPConn: conn,
		UDPAddr: addr,
		Process: cmd,
		State:   make(map[string]*gamepb.PlayerState),
		WebRTC:  webrtc.NewManager(roomID),
	}
	b.gameRooms[roomID] = gr

	// Start receiving for this room
	go b.receiveUDP(gr)
	
	// Start WebRTC track handler
	go b.handleWebRTCTracks(gr)

	log.Printf("🚀 Spawned game server for room %s on UDP :%d", roomID, port)
	return gr, nil
}

func (b *Bridge) receiveUDP(gr *GameRoom) {
	buf := make([]byte, 4096)
	for {
		n, err := gr.UDPConn.Read(buf)
		if err != nil {
			log.Printf("UDP read error for room %s: %v", gr.ID, err)
			return
		}

		msg, err := protocol.Decode(buf[:n])
		if err != nil {
			continue
		}

		switch payload := msg.Payload.(type) {
		case *gamepb.Message_ServerWelcome:
			log.Printf("🎮 Room %s: Welcome! Player ID: %s", gr.ID, payload.ServerWelcome.PlayerId)

		case *gamepb.Message_StateDelta:
			if payload.StateDelta != nil {
				gr.Mu.Lock()
				for _, p := range payload.StateDelta.ChangedPlayers {
					gr.State[p.PlayerId] = p
				}
				for _, id := range payload.StateDelta.RemovedPlayers {
					delete(gr.State, id)
				}
				gr.Mu.Unlock()
				b.broadcastRoomState(gr)
			}

		case *gamepb.Message_StateSnapshot:
			if payload.StateSnapshot != nil {
				gr.Mu.Lock()
				gr.State = make(map[string]*gamepb.PlayerState)
				for _, p := range payload.StateSnapshot.Players {
					gr.State[p.PlayerId] = p
				}
				gr.Mu.Unlock()
				b.broadcastRoomState(gr)
			}
		}
	}
}

type PlayerMsg struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	X    float32 `json:"x"`
	Y    float32 `json:"y"`
	VX   float32 `json:"vx"`
	VY   float32 `json:"vy"`
	Rot  float32 `json:"rot"`
}

type StateMsg struct {
	Type    string      `json:"type"`
	YourID  string      `json:"yourId"`
	RoomID  string      `json:"roomId,omitempty"`
	Players []PlayerMsg `json:"players"`
}

// broadcastRoomState sends state only to players in this room
func (b *Bridge) broadcastRoomState(gr *GameRoom) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	gr.Mu.RLock()
	players := make([]PlayerMsg, 0, len(gr.State))
	for id, p := range gr.State {
		x, y := float32(500), float32(500)
		vx, vy := float32(0), float32(0)
		if p.Position != nil {
			x, y = p.Position.X, p.Position.Y
		}
		if p.Velocity != nil {
			vx, vy = p.Velocity.X, p.Velocity.Y
		}
		players = append(players, PlayerMsg{
			ID:  id,
			X:   x,
			Y:   y,
			VX:  vx,
			VY:  vy,
			Rot: p.Rotation,
		})
	}
	gr.Mu.RUnlock()

	for ws, client := range b.clients {
		if client.roomID == gr.ID {
			state := StateMsg{
				Type:    "state",
				YourID:  client.playerID,
				RoomID:  gr.ID,
				Players: players,
			}
			ws.WriteJSON(state)
		}
	}
}

// broadcastToRoom sends a message to all clients in a room
func (b *Bridge) broadcastToRoom(roomID string, msg interface{}) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ws, client := range b.clients {
		if client.roomID == roomID {
			ws.WriteJSON(msg)
		}
	}
}

// stopGameRoom kills the game server process for a room
func (b *Bridge) stopGameRoom(roomID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if gr, exists := b.gameRooms[roomID]; exists {
		if gr.Process != nil && gr.Process.Process != nil {
			gr.Process.Process.Kill()
			gr.UDPConn.Close()
		}
		delete(b.gameRooms, roomID)
		log.Printf("🛑 Stopped game server for room %s", roomID)
	}
}

// ================== HTTP API ==================

type CreateRoomResponse struct {
	RoomID    string `json:"roomId"`
	JoinLink  string `json:"joinLink"`
	CreatedAt int64  `json:"createdAt"`
	HostID    string `json:"hostId"`
}

type RoomInfoResponse struct {
	RoomID      string   `json:"roomId"`
	PlayerCount int      `json:"playerCount"`
	MaxPlayers  int      `json:"maxPlayers"`
	Players     []string `json:"players"`
	CreatedAt   int64    `json:"createdAt"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func (b *Bridge) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "method not allowed"})
		return
	}

	rm := b.rooms.Create()
	host := r.URL.Query().Get("host")
	if host == "" {
		host = uuid.New().String()[:8]
	}

	rm.Join(host, "Host")

	// Build join link from request host
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	joinLink := fmt.Sprintf("%s://%s/room/%s", scheme, r.Host, rm.ID)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(CreateRoomResponse{
		RoomID:    rm.ID,
		JoinLink:  joinLink,
		CreatedAt: rm.CreatedAt.Unix(),
		HostID:    host,
	})

	log.Printf("🏠 Room created: %s (host: %s)", rm.ID, host)
}

func (b *Bridge) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rooms/"), "/")
	roomID := parts[0]
	if roomID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "room id required"})
		return
	}

	rm := b.rooms.Get(roomID)
	if rm == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "room not found"})
		return
	}

	playerIDs := rm.PlayerIDs()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(RoomInfoResponse{
		RoomID:      rm.ID,
		PlayerCount: len(playerIDs),
		MaxPlayers:  rm.MaxPlayer,
		Players:     playerIDs,
		CreatedAt:   rm.CreatedAt.Unix(),
	})
}

func (b *Bridge) handleDeleteRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rooms/"), "/")
	roomID := parts[0]

	rm := b.rooms.Get(roomID)
	if rm == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "room not found"})
		return
	}

	// Stop game server
	b.stopGameRoom(roomID)
	b.rooms.Delete(roomID)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ================== WebSocket ==================

func (b *Bridge) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	client := &BrowserClient{
		ws:       conn,
		playerID: uuid.New().String()[:8],
		name:     "Player",
		roomID:   "",
	}

	b.mu.Lock()
	b.clients[conn] = client
	b.mu.Unlock()

	log.Printf("📱 Browser connected: %s", client.playerID)

	conn.WriteJSON(map[string]interface{}{
		"type": "welcome",
		"id":   client.playerID,
	})

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var data map[string]interface{}
		if err := json.Unmarshal(msg, &data); err != nil {
			continue
		}

		switch data["type"] {
		case "input":
			if client.roomID == "" {
				continue
			}

			b.mu.RLock()
			gr, exists := b.gameRooms[client.roomID]
			b.mu.RUnlock()

			if !exists {
				continue
			}

			dx, _ := data["dx"].(float64)
			dy, _ := data["dy"].(float64)
			ts := uint64(time.Now().UnixMilli())

			input := protocol.NewPlayerInput(client.playerID, ts, ts, float32(dx), float32(dy), false, false, false)
			if inputData, err := protocol.Encode(input); err == nil {
				gr.UDPConn.Write(inputData)
			}

		case "join_room":
			roomID, _ := data["roomId"].(string)
			playerName, _ := data["name"].(string)
			if playerName == "" {
				playerName = "Player"
			}

			rm, player, err := b.rooms.Join(roomID, client.playerID, playerName)
			if err != nil {
				conn.WriteJSON(map[string]interface{}{
					"type":  "error",
					"error": err.Error(),
				})
				continue
			}

			client.roomID = roomID
			client.name = playerName

			// Spawn game server for this room
			gr, err := b.spawnGameServer(roomID)
			if err != nil {
				conn.WriteJSON(map[string]interface{}{
					"type":  "error",
					"error": "failed to start game server",
				})
				continue
			}

			// Send hello to game server
			hello := protocol.NewClientHello(client.playerID, client.name, "1.0")
			if helloData, err := protocol.Encode(hello); err == nil {
				gr.UDPConn.Write(helloData)
			}

			conn.WriteJSON(map[string]interface{}{
				"type":        "room_joined",
				"roomId":      roomID,
				"playerId":    client.playerID,
				"isHost":      player.IsHost,
				"playerCount": rm.PlayerCount(),
			})

			b.broadcastToRoom(roomID, map[string]interface{}{
				"type":        "player_joined",
				"playerId":    client.playerID,
				"playerName":  playerName,
				"playerCount": rm.PlayerCount(),
			})

			log.Printf("🚪 %s joined room %s (%d players)", client.playerID, roomID, rm.PlayerCount())

		case "leave_room":
			if client.roomID != "" {
				rm := b.rooms.Get(client.roomID)
				if rm != nil {
					rm.Leave(client.playerID)
					b.broadcastToRoom(client.roomID, map[string]interface{}{
						"type":       "player_left",
						"playerId":   client.playerID,
						"playerName": client.name,
					})
				}
				client.roomID = ""
			}

		// WebRTC Signaling
		case "webrtc_offer":
			if client.roomID == "" {
				continue
			}
			b.mu.RLock()
			gr, exists := b.gameRooms[client.roomID]
			b.mu.RUnlock()

			if !exists {
				continue
			}

			sdp, _ := data["sdp"].(string)
			answer, err := gr.WebRTC.HandleOffer(client.playerID, sdp)
			if err != nil {
				log.Printf("❌ WebRTC offer error: %v", err)
				conn.WriteJSON(map[string]interface{}{
					"type":  "webrtc_error",
					"error": err.Error(),
				})
				continue
			}

			// Send answer back to client
			conn.WriteJSON(map[string]interface{}{
				"type":     "webrtc_answer",
				"roomId":   client.roomID,
				"playerId": client.playerID,
				"sdp":      answer.SDP,
			})
			
			log.Printf("✅ [%s] Sent WebRTC answer, senders: %d", client.playerID, gr.WebRTC.GetSenders(client.playerID))

		case "webrtc_answer":
			if client.roomID == "" {
				continue
			}
			b.mu.RLock()
			gr, exists := b.gameRooms[client.roomID]
			b.mu.RUnlock()

			if !exists {
				continue
			}

			sdp, _ := data["sdp"].(string)
			if err := gr.WebRTC.HandleAnswer(client.playerID, sdp); err != nil {
				log.Printf("❌ WebRTC answer error: %v", err)
			}

		case "webrtc_ice":
			if client.roomID == "" {
				continue
			}
			b.mu.RLock()
			gr, exists := b.gameRooms[client.roomID]
			b.mu.RUnlock()

			if !exists {
				continue
			}

			candidate, _ := data["candidate"].(json.RawMessage)
			if err := gr.WebRTC.HandleICECandidate(client.playerID, candidate); err != nil {
				log.Printf("❌ WebRTC ICE error: %v", err)
			}
		}
	}

	// Cleanup
	b.mu.Lock()
	delete(b.clients, conn)
	roomID := client.roomID
	b.mu.Unlock()

	if roomID != "" {
		rm := b.rooms.Get(roomID)
		if rm != nil {
			rm.Leave(client.playerID)
			b.broadcastToRoom(roomID, map[string]interface{}{
				"type":       "player_left",
				"playerId":   client.playerID,
				"playerName": client.name,
			})
		}
	}

	log.Printf("📱 Browser disconnected: %s", client.playerID)
}

func (b *Bridge) handleStatus(w http.ResponseWriter, r *http.Request) {
	b.mu.RLock()
	clientCount := len(b.clients)
	roomCount := len(b.gameRooms)
	b.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"browser_clients":%d,"game_rooms":%d,"rooms":%d}`, clientCount, roomCount, b.rooms.Count())
}

// handleWebRTCTracks handles incoming WebRTC tracks and renegotiation
func (b *Bridge) handleWebRTCTracks(gr *GameRoom) {
	log.Printf("🎥 [DEBUG] Starting WebRTC track handler for room %s", gr.ID)
	for {
		select {
		case event := <-gr.WebRTC.GetTrackEvents():
			log.Printf("🎥 [TRACK EVENT] from %s in room %s, kind: %v", event.PlayerID, gr.ID, event.Track.Kind())
			
		case renegotiate := <-gr.WebRTC.GetRenegotiateChan():
			log.Printf("🔄 [RENEGOTIATE] Starting for player %s, kind: %v", renegotiate.PlayerID, renegotiate.Kind)
			
			// Create new offer for this player
			offer, err := gr.WebRTC.CreateOffer(renegotiate.PlayerID)
			if err != nil {
				log.Printf("❌ [RENEGOTIATE] Failed to create offer: %v", err)
				continue
			}
			if offer == nil {
				log.Printf("❌ [RENEGOTIATE] Offer is nil for %s", renegotiate.PlayerID)
				continue
			}
			
			log.Printf("🔄 [RENEGOTIATE] Created offer, SDP length: %d", len(offer.SDP))
			
			// Find the WebSocket connection for this player
			b.mu.RLock()
			var targetConn *websocket.Conn
			var foundClient *BrowserClient
			for ws, client := range b.clients {
				if client.playerID == renegotiate.PlayerID && client.roomID == gr.ID {
					targetConn = ws
					foundClient = client
					break
				}
			}
			b.mu.RUnlock()
			
			if targetConn == nil {
				log.Printf("⚠️ [RENEGOTIATE] No connection found for player %s", renegotiate.PlayerID)
				continue
			}
			
			log.Printf("🔄 [RENEGOTIATE] Found connection for %s (name: %s)", renegotiate.PlayerID, foundClient.name)
			
			// Send offer to client
			err = targetConn.WriteJSON(map[string]interface{}{
				"type":     "webrtc_offer",
				"roomId":   gr.ID,
				"playerId": renegotiate.PlayerID,
				"sdp":      offer.SDP,
			})
			if err != nil {
				log.Printf("❌ [RENEGOTIATE] Failed to send offer: %v", err)
			} else {
				log.Printf("📤 [RENEGOTIATE] Sent offer to %s", renegotiate.PlayerID)
			}
		}
	}
}

func (b *Bridge) handleLanding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(landingPageHTML))
}

func (b *Bridge) handleRoomPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(roomPageHTML))
}

func main() {
	bridge := NewBridge()

	fs := http.FileServer(http.Dir("./cmd/webbridge/public"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
	http.HandleFunc("/rooms", bridge.handleCreateRoom)
	http.HandleFunc("/rooms/", bridge.handleRoomRoutes)
	http.HandleFunc("/ws", bridge.handleWS)
	http.HandleFunc("/status", bridge.handleStatus)
	http.HandleFunc("/", bridge.handleLanding)
	http.HandleFunc("/room/", bridge.handleRoomPage)

	// Get port from environment (Render sets PORT)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	// Check for HTTPS certs (local dev)
	certFile := "certs/localhost+2.pem"
	keyFile := "certs/localhost+2-key.pem"
	
	_, certErr := os.Stat(certFile)
	_, keyErr := os.Stat(keyFile)
	
	// Check if we're behind a proxy (Render provides HTTPS)
	_, isRender := os.LookupEnv("RENDER")
	
	if isRender || (certErr != nil || keyErr != nil) {
		// HTTP only (Render handles HTTPS termination, or local dev without certs)
		log.Printf("🌐 Web Bridge: http://localhost:%s", port)
		log.Println("📡 Game servers will be spawned per-room starting at port 9100")
		log.Println("🎥 WebRTC enabled (native Pion)")
		log.Fatal(http.ListenAndServe(":"+port, nil))
	} else {
		// HTTPS available (local dev with mkcert)
		log.Println("🔐 HTTPS enabled")
		log.Println("🌐 Web Bridge: https://localhost:8443")
		log.Println("🌐 Also: https://192.168.0.39:8443")
		log.Println("📡 Game servers will be spawned per-room starting at port 9100")
		log.Println("🎥 WebRTC enabled (native Pion)")
		log.Fatal(http.ListenAndServeTLS(":8443", certFile, keyFile, nil))
	}
}

func (b *Bridge) handleRoomRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetRoom(w, r)
	case http.MethodDelete:
		b.handleDeleteRoom(w, r)
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, DELETE, OPTIONS")
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

var landingPageHTML = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>GameServer</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            background: #0a0a0f;
            color: #ddd;
            font-family: 'Inter', -apple-system, sans-serif;
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
        }
        .container { text-align: center; padding: 40px; }
        h1 {
            font-size: 3rem;
            background: linear-gradient(135deg, #00d4ff, #7c3aed);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 1rem;
        }
        p { color: #888; margin-bottom: 2rem; font-size: 1.1rem; }
        .btn {
            background: linear-gradient(135deg, #00d4ff, #00a8cc);
            color: #000;
            border: none;
            padding: 16px 48px;
            font-size: 1.2rem;
            border-radius: 8px;
            cursor: pointer;
            font-weight: 600;
            transition: transform 0.2s, box-shadow 0.2s;
        }
        .btn:hover { transform: translateY(-2px); box-shadow: 0 8px 24px rgba(0, 212, 255, 0.3); }
        .btn:active { transform: translateY(0); }
        .footer { margin-top: 3rem; color: #555; font-size: 0.9rem; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🎮 GameServer</h1>
        <p>Create a room and invite your friends</p>
        <button class="btn" onclick="createRoom()">Create Room</button>
        <div class="footer"><p>Multiplayer game server • Voice & Video enabled</p></div>
    </div>
    <script>
        async function createRoom() {
            const res = await fetch('/rooms', { method: 'POST' });
            const data = await res.json();
            window.location.href = data.joinLink;
        }
    </script>
</body>
</html>
`

var roomPageHTML = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Room</title>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <div id="app">
        <div id="video-panel">
            <div id="video-header">
                <span id="room-display">Room: <span id="room-id">Loading...</span></span>
                <div id="video-controls">
                    <button id="btn-mic" class="control-btn" onclick="toggleMic()">🎤</button>
                    <button id="btn-cam" class="control-btn" onclick="toggleCam()">📷</button>
                </div>
            </div>
            <div id="video-grid"></div>
        </div>
        <canvas id="game"></canvas>
        <div id="hud">
            <div class="hud-row">
                <span class="hud-label">Player:</span>
                <span class="hud-value" id="player-id">Loading...</span>
            </div>
            <div class="hud-row">
                <span class="hud-label">Players:</span>
                <span class="hud-value" id="player-count">0</span>
            </div>
        </div>
        <div id="share">
            <span>Share link:</span>
            <input type="text" id="share-link" readonly>
            <button onclick="copyLink()">📋</button>
        </div>
    </div>
    <script src="/static/game.js"></script>
</body>
</html>
`