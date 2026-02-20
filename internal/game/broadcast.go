package game

import (
	"log"

	"github.com/LemmyAI/gameserver/internal/protocol"
	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
)

// TransportBroadcaster implements Broadcaster using a Transport.
type TransportBroadcaster struct {
	state *State
	send  func(addr string, data []byte) error
}

// NewTransportBroadcaster creates a broadcaster using a send function.
func NewTransportBroadcaster(state *State, send func(addr string, data []byte) error) *TransportBroadcaster {
	return &TransportBroadcaster{
		state: state,
		send:  send,
	}
}

// SetState sets the game state (for late initialization).
func (b *TransportBroadcaster) SetState(state *State) {
	b.state = state
}

// Broadcast sends a message to all connected players.
func (b *TransportBroadcaster) Broadcast(msg *gamepb.Message, excludeID string) error {
	data, err := protocol.Encode(msg)
	if err != nil {
		return err
	}

	players := b.state.AllPlayers()
	for _, p := range players {
		if p.ID == excludeID {
			continue
		}
		if err := b.send(p.Addr, data); err != nil {
			log.Printf("Broadcast error to %s: %v", p.Addr, err)
		}
	}
	return nil
}

// SendTo sends a message to a specific address.
func (b *TransportBroadcaster) SendTo(addr string, msg *gamepb.Message) error {
	data, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	return b.send(addr, data)
}