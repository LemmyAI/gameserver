// Command client is a simple test client for the game server.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/LemmyAI/gameserver/internal/protocol"
	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
)

func main() {
	serverAddr := flag.String("addr", "localhost:9000", "server address")
	playerName := flag.String("name", "TestPlayer", "player name")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Resolve server address
	raddr, err := net.ResolveUDPAddr("udp", *serverAddr)
	if err != nil {
		log.Fatalf("Resolve addr: %v", err)
	}

	// Create local UDP socket
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		log.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	log.Printf("🎮 Connecting to %s as %s...", *serverAddr, *playerName)

	// Generate client-side player ID
	playerID := fmt.Sprintf("client-%d", time.Now().UnixNano()%100000)

	// Send ClientHello
	hello := protocol.NewClientHello(
		playerID,
		*playerName,
		"0.1.0",
	)

	data, err := protocol.Encode(hello)
	if err != nil {
		log.Fatalf("Encode: %v", err)
	}

	_, err = conn.Write(data)
	if err != nil {
		log.Fatalf("Write: %v", err)
	}
	log.Printf("📤 Sent ClientHello")

	log.Printf("📤 Sent ClientHello")

	// Start receive goroutine
	go func() {
		buf := make([]byte, 1400)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, err := conn.Read(buf)
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue
					}
					log.Printf("Read error: %v", err)
					return
				}

				msg, err := protocol.Decode(buf[:n])
				if err != nil {
					log.Printf("⚠️  Invalid message: %v", err)
					continue
				}

				switch p := msg.Payload.(type) {
				case *gamepb.Message_ServerWelcome:
					log.Printf("✅ ServerWelcome: player_id=%s, tick_rate=%d",
						p.ServerWelcome.PlayerId, p.ServerWelcome.TickRate)
				case *gamepb.Message_StateSnapshot:
					log.Printf("📊 StateSnapshot: tick=%d, players=%d",
						p.StateSnapshot.Tick, len(p.StateSnapshot.Players))
				default:
					log.Printf("📥 Received: %s", protocol.MessageTypeName(msg))
				}
			}
		}
	}()

	// Read input and send
	fmt.Println("\n🎮 Use arrow keys (or WASD) to move. Press Enter to send. Type 'quit' to exit.")
	fmt.Println("   Commands: up, down, left, right, jump, quit")

	scanner := bufio.NewScanner(os.Stdin)
	seq := uint64(0)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "quit" {
			break
		}

		seq++
		var x, y float32
		var jump bool

		switch line {
		case "up", "w":
			y = -1
		case "down", "s":
			y = 1
		case "left", "a":
			x = -1
		case "right", "d":
			x = 1
		case "jump":
			jump = true
		default:
			seq-- // don't increment for invalid commands
			continue
		}

		input := protocol.NewPlayerInput(playerID, seq, uint64(time.Now().UnixMilli()), x, y, jump, false, false)
		data, err := protocol.Encode(input)
		if err != nil {
			log.Printf("Encode error: %v", err)
			continue
		}

		_, err = conn.Write(data)
		if err != nil {
			log.Printf("Write error: %v", err)
			continue
		}
		log.Printf("📤 Sent input: move=(%.1f,%.1f) jump=%v", x, y, jump)
	}

	log.Println("👋 Goodbye!")
}