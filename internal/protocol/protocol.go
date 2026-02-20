// Package protocol provides helpers for encoding/decoding game messages.
package protocol

import (
	"fmt"

	"github.com/LemmyAI/gameserver/internal/protocol/gamepb"
	"google.golang.org/protobuf/proto"
)

// Encode serializes a Message to bytes.
func Encode(msg *gamepb.Message) ([]byte, error) {
	return proto.Marshal(msg)
}

// Decode deserializes bytes to a Message.
func Decode(data []byte) (*gamepb.Message, error) {
	msg := &gamepb.Message{}
	if err := proto.Unmarshal(data, msg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return msg, nil
}

// NewClientHello creates a ClientHello message wrapped in Message.
func NewClientHello(playerID, playerName, version string) *gamepb.Message {
	return &gamepb.Message{
		Payload: &gamepb.Message_ClientHello{
			ClientHello: &gamepb.ClientHello{
				PlayerId:   playerID,
				PlayerName: playerName,
				Version:    version,
			},
		},
	}
}

// NewServerWelcome creates a ServerWelcome message wrapped in Message.
func NewServerWelcome(playerID string, tickRate uint32, serverTime uint64) *gamepb.Message {
	return &gamepb.Message{
		Payload: &gamepb.Message_ServerWelcome{
			ServerWelcome: &gamepb.ServerWelcome{
				PlayerId:   playerID,
				TickRate:   tickRate,
				ServerTime: serverTime,
			},
		},
	}
}

// NewPlayerInput creates a PlayerInput message wrapped in Message.
func NewPlayerInput(sequence, timestamp uint64, x, y float32, jump, action1, action2 bool) *gamepb.Message {
	return &gamepb.Message{
		Payload: &gamepb.Message_PlayerInput{
			PlayerInput: &gamepb.PlayerInput{
				Sequence:  sequence,
				Timestamp: timestamp,
				Movement: &gamepb.Vec2{
					X: float32(x),
					Y: float32(y),
				},
				Jump:    jump,
				Action_1: action1,
				Action_2: action2,
			},
		},
	}
}

// NewPlayerState creates a PlayerState.
func NewPlayerState(playerID string, x, y, vx, vy, rotation float32, timestamp uint64) *gamepb.PlayerState {
	return &gamepb.PlayerState{
		PlayerId: playerID,
		Position: &gamepb.Vec2{X: x, Y: y},
		Velocity: &gamepb.Vec2{X: vx, Y: vy},
		Rotation: rotation,
		Timestamp: timestamp,
	}
}

// MessageTypeName returns a human-readable name for the message type.
func MessageTypeName(msg *gamepb.Message) string {
	switch msg.Payload.(type) {
	case *gamepb.Message_ClientHello:
		return "ClientHello"
	case *gamepb.Message_ServerWelcome:
		return "ServerWelcome"
	case *gamepb.Message_PlayerInput:
		return "PlayerInput"
	case *gamepb.Message_StateSnapshot:
		return "StateSnapshot"
	case *gamepb.Message_StateDelta:
		return "StateDelta"
	case *gamepb.Message_PlayerJoin:
		return "PlayerJoin"
	case *gamepb.Message_PlayerLeave:
		return "PlayerLeave"
	default:
		return "Unknown"
	}
}