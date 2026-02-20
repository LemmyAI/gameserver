// WebBridge - WebSocket to UDP bridge for browser testing
// Browser (WebSocket) ↔ WebBridge ↔ GameServer (UDP/Protobuf)
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/google/uuid"

	"github.com/LemmyAI/gameserver/internal/protocol"
	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type BrowserClient struct {
	ws       *websocket.Conn
	playerID string
	name     string
}

type Bridge struct {
	clients   map[*websocket.Conn]*BrowserClient
	udpConn   *net.UDPConn
	mu        sync.RWMutex
	gameState map[string]*gamepb.PlayerState
}

func NewBridge() *Bridge {
	return &Bridge{
		clients:   make(map[*websocket.Conn]*BrowserClient),
		gameState: make(map[string]*gamepb.PlayerState),
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
	ID  string  `json:"id"`
	X   float32 `json:"x"`
	Y   float32 `json:"y"`
	VX  float32 `json:"vx"`
	VY  float32 `json:"vy"`
	Rot float32 `json:"rot"`
}

type StateMsg struct {
	Type    string       `json:"type"`
	YourID  string       `json:"yourId"`
	Players []PlayerMsg  `json:"players"`
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
			Players: players,
		}
		ws.WriteJSON(state)
	}
}

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
		}
	}

	b.mu.Lock()
	delete(b.clients, conn)
	b.mu.Unlock()
	log.Printf("📱 Browser disconnected: %s", client.playerID)
}

func (b *Bridge) handleStatus(w http.ResponseWriter, r *http.Request) {
	b.mu.RLock()
	count := len(b.clients)
	pCount := len(b.gameState)
	b.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"browser_clients":%d,"players":%d}`, count, pCount)
}

func main() {
	bridge := NewBridge()

	if err := bridge.connectToGameServer(); err != nil {
		log.Fatalf("❌ Failed to connect to game server: %v", err)
	}
	log.Println("✅ Connected to UDP :9000")

	http.Handle("/", http.FileServer(http.Dir("./cmd/webbridge/public")))
	http.HandleFunc("/ws", bridge.handleWS)
	http.HandleFunc("/status", bridge.handleStatus)

	log.Println("🌐 Web Bridge: http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}