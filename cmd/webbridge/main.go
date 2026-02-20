// WebBridge - WebSocket to UDP bridge with room support
// Browser (WebSocket) ↔ WebBridge ↔ GameServer (UDP/Protobuf)
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/google/uuid"

	"github.com/LemmyAI/gameserver/internal/protocol"
	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
	"github.com/LemmyAI/gameserver/internal/room"
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

type Bridge struct {
	clients   map[*websocket.Conn]*BrowserClient
	udpConn   *net.UDPConn
	mu        sync.RWMutex
	gameState map[string]*gamepb.PlayerState
	rooms     *room.Registry
}

func NewBridge() *Bridge {
	config := room.DefaultConfig()
	return &Bridge{
		clients:   make(map[*websocket.Conn]*BrowserClient),
		gameState: make(map[string]*gamepb.PlayerState),
		rooms:     room.NewRegistry(config),
	}
}

func (b *Bridge) connectToGameServer() error {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:9000")
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return err
	}

	b.udpConn = conn
	go b.receiveUDP()
	return nil
}

func (b *Bridge) receiveUDP() {
	buf := make([]byte, 4096)
	for {
		n, err := b.udpConn.Read(buf)
		if err != nil {
			log.Printf("UDP read error: %v", err)
			continue
		}

		msg, err := protocol.Decode(buf[:n])
		if err != nil {
			continue
		}

		switch payload := msg.Payload.(type) {
		case *gamepb.Message_ServerWelcome:
			log.Printf("🎮 Welcome! Player ID: %s", payload.ServerWelcome.PlayerId)
		case *gamepb.Message_StateDelta:
			if payload.StateDelta != nil {
				b.mu.Lock()
				for _, p := range payload.StateDelta.ChangedPlayers {
					b.gameState[p.PlayerId] = p
				}
				for _, id := range payload.StateDelta.RemovedPlayers {
					delete(b.gameState, id)
				}
				b.mu.Unlock()
				b.broadcastState()
			}
		case *gamepb.Message_StateSnapshot:
			if payload.StateSnapshot != nil {
				b.mu.Lock()
				b.gameState = make(map[string]*gamepb.PlayerState)
				for _, p := range payload.StateSnapshot.Players {
					b.gameState[p.PlayerId] = p
				}
				b.mu.Unlock()
				b.broadcastState()
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

func (b *Bridge) broadcastState() {
	b.mu.RLock()
	defer b.mu.RUnlock()

	players := make([]PlayerMsg, 0, len(b.gameState))
	for id, p := range b.gameState {
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

	for ws, client := range b.clients {
		state := StateMsg{
			Type:    "state",
			YourID:  client.playerID,
			RoomID:  client.roomID,
			Players: players,
		}
		ws.WriteJSON(state)
	}
}

// broadcastToRoom sends state only to players in a specific room
func (b *Bridge) broadcastToRoom(roomID string, msg interface{}) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ws, client := range b.clients {
		if client.roomID == roomID {
			ws.WriteJSON(msg)
		}
	}
}

// ================== HTTP API ==================

// CreateRoomResponse is returned when creating a room
type CreateRoomResponse struct {
	RoomID    string `json:"roomId"`
	JoinLink  string `json:"joinLink"`
	CreatedAt int64  `json:"createdAt"`
	HostID    string `json:"hostId"`
}

// RoomInfoResponse contains room details
type RoomInfoResponse struct {
	RoomID      string   `json:"roomId"`
	PlayerCount int      `json:"playerCount"`
	MaxPlayers  int      `json:"maxPlayers"`
	Players     []string `json:"players"`
	CreatedAt   int64    `json:"createdAt"`
}

// ErrorResponse for API errors
type ErrorResponse struct {
	Error string `json:"error"`
}

func (b *Bridge) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "method not allowed"})
		return
	}

	// Create room
	rm := b.rooms.Create()

	// Build response
	host := r.URL.Query().Get("host")
	if host == "" {
		host = uuid.New().String()[:8]
	}

	// Auto-join host to room
	rm.Join(host, "Host")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(CreateRoomResponse{
		RoomID:    rm.ID,
		JoinLink:  fmt.Sprintf("http://localhost:8081/room/%s", rm.ID),
		CreatedAt: rm.CreatedAt.Unix(),
		HostID:    host,
	})

	log.Printf("🏠 Room created: %s (host: %s)", rm.ID, host)
}

func (b *Bridge) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	// Extract room ID from path: /rooms/{id}
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
		json.NewEncoder(w).Encode(ErrorResponse{Error: "method not allowed"})
		return
	}

	// Extract room ID from path
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rooms/"), "/")
	roomID := parts[0]

	rm := b.rooms.Get(roomID)
	if rm == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "room not found"})
		return
	}

	// TODO: Check if requester is host

	b.rooms.Delete(roomID)
	log.Printf("🗑️  Room deleted: %s", roomID)

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
		case "hello":
			client.name, _ = data["name"].(string)
			hello := protocol.NewClientHello(client.playerID, client.name, "1.0")
			helloData, _ := protocol.Encode(hello)
			b.udpConn.Write(helloData)

		case "input":
			dx, _ := data["dx"].(float64)
			dy, _ := data["dy"].(float64)
			ts := uint64(time.Now().UnixMilli())
			seq := ts

			input := protocol.NewPlayerInput(client.playerID, seq, ts, float32(dx), float32(dy), false, false, false)
			inputData, _ := protocol.Encode(input)
			b.udpConn.Write(inputData)

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

			// Send room joined confirmation
			conn.WriteJSON(map[string]interface{}{
				"type":        "room_joined",
				"roomId":      roomID,
				"playerId":    client.playerID,
				"isHost":      player.IsHost,
				"playerCount": rm.PlayerCount(),
			})

			// Notify others in room
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
					log.Printf("🚪 %s left room %s", client.playerID, client.roomID)
				}
				client.roomID = ""
			}
		}
	}

	// Cleanup on disconnect
	b.mu.Lock()
	delete(b.clients, conn)
	b.mu.Unlock()

	// Leave room if in one
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
	}

	log.Printf("📱 Browser disconnected: %s", client.playerID)
}

func (b *Bridge) handleStatus(w http.ResponseWriter, r *http.Request) {
	b.mu.RLock()
	clientCount := len(b.clients)
	pCount := len(b.gameState)
	b.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"browser_clients":%d,"players":%d,"rooms":%d}`, clientCount, pCount, b.rooms.Count())
}

// handleLanding serves the landing page with "Create Room" button
func (b *Bridge) handleLanding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(landingPageHTML))
}

// handleRoomPage serves the room page (game canvas + video grid)
func (b *Bridge) handleRoomPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(roomPageHTML))
}

func main() {
	bridge := NewBridge()

	if err := bridge.connectToGameServer(); err != nil {
		log.Fatalf("❌ Failed to connect to game server: %v", err)
	}
	log.Println("✅ Connected to UDP :9000")

	// Static files (CSS, JS assets)
	fs := http.FileServer(http.Dir("./cmd/webbridge/public"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// API endpoints
	http.HandleFunc("/rooms", bridge.handleCreateRoom)           // POST /rooms
	http.HandleFunc("/rooms/", bridge.handleRoomRoutes)          // GET/DELETE /rooms/{id}

	// WebSocket
	http.HandleFunc("/ws", bridge.handleWS)

	// Status endpoint
	http.HandleFunc("/status", bridge.handleStatus)

	// Pages
	http.HandleFunc("/", bridge.handleLanding)                   // Landing page
	http.HandleFunc("/room/", bridge.handleRoomPage)             // Room page /room/{id}

	log.Println("🌐 Web Bridge: http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

// handleRoomRoutes dispatches to GET or DELETE for /rooms/{id}
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

// HTML templates are in a separate file
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
        .container {
            text-align: center;
            padding: 40px;
        }
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
        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 24px rgba(0, 212, 255, 0.3);
        }
        .btn:active { transform: translateY(0); }
        .footer {
            margin-top: 3rem;
            color: #555;
            font-size: 0.9rem;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🎮 GameServer</h1>
        <p>Create a room and invite your friends</p>
        <button class="btn" onclick="createRoom()">Create Room</button>
        <div class="footer">
            <p>Multiplayer game server • Voice & Video enabled</p>
        </div>
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

// roomPageHTML is loaded when joining a room /room/{id}
var roomPageHTML = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Room</title>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <div id="app">
        <canvas id="game"></canvas>
        <div id="hud">
            <div class="hud-row">
                <span class="hud-label">Room:</span>
                <span class="hud-value" id="room-id">Loading...</span>
            </div>
            <div class="hud-row">
                <span class="hud-label">Player:</span>
                <span class="hud-value" id="player-id">Loading...</span>
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