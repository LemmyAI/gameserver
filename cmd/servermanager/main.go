package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
)

// GameServerProcess manages a child game server process for one room
type GameServerProcess struct {
	RoomID    string
	Port      int
	Process   *exec.Cmd
	UDPAddr   *net.UDPAddr
	mu        sync.RWMutex
}

// GameServerManager spawns and manages game server instances
type GameServerManager struct {
	servers  map[int]*GameServerProcess
	portMux  sync.Mutex
	nextPort int
	mu       sync.RWMutex
}

func NewGameServerManager() *GameServerManager {
	return &GameServerManager{
		servers:  make(map[int]*GameServerProcess),
		nextPort: 9100, // Start at 9100, increment for each room
	}
}

// Spawn creates a new game server process for a room
func (g *GameServerManager) Spawn(roomID string) (*GameServerProcess, error) {
	g.portMux.Lock()
	port := g.nextPort
	g.nextPort++
	g.portMux.Unlock()

	// Build command to spawn server
	cmd := exec.Command("./bin/server",
		"-port", fmt.Sprintf("%d", port),
		"-http", fmt.Sprintf("%d", port+1000), // HTTP on port+1000
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set environment to identify this server
	cmd.Env = append(os.Environ(), fmt.Sprintf("ROOM_ID=%s", roomID))

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start server: %w", err)
	}

	// Resolve UDP address
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	gs := &GameServerProcess{
		RoomID:  roomID,
		Port:    port,
		Process: cmd,
		UDPAddr: addr,
	}

	g.mu.Lock()
	g.servers[port] = gs
	g.mu.Unlock()

	log.Printf("🚀 Spawned game server for room %s on UDP :%d", roomID, port)
	return gs, nil
}

// Get retrieves a game server by port
func (g *GameServerManager) Get(port int) *GameServerProcess {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.servers[port]
}

// Kill stops a game server process
func (gs *GameServerProcess) Kill() error {
	if gs.Process != nil && gs.Process.Process != nil {
		return gs.Process.Process.Kill()
	}
	return nil
}
