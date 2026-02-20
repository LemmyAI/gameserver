// Package transport provides a network abstraction layer.
// This allows swapping UDP, QUIC, or mock implementations without changing game logic.
package transport

import (
	"time"
)

// Transport is the interface for network communication.
type Transport interface {
	// Listen starts listening on the given address.
	Listen(addr string) error

	// Close shuts down the transport.
	Close() error

	// SendUnreliable sends data without guaranteed delivery (UDP-style).
	SendUnreliable(addr string, data []byte) error

	// SendReliable sends data with guaranteed delivery (ACK/retry).
	SendReliable(addr string, data []byte) error

	// OnMessage registers a handler for incoming messages.
	OnMessage(handler MessageHandler)

	// OnConnect registers a handler for new connections.
	OnConnect(handler ConnectHandler)

	// OnDisconnect registers a handler for disconnections.
	OnDisconnect(handler DisconnectHandler)

	// LocalAddr returns the local address we're listening on.
	LocalAddr() string
}

// MessageHandler is called when a message is received.
type MessageHandler func(addr string, data []byte, reliable bool)

// ConnectHandler is called when a new client connects.
type ConnectHandler func(addr string)

// DisconnectHandler is called when a client disconnects.
type DisconnectHandler func(addr string)

// Config holds transport configuration.
type Config struct {
	MaxMessageSize int
	SendBufferSize int
	RecvBufferSize int
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxMessageSize: 1400, // Safe for UDP
		SendBufferSize: 1024,
		RecvBufferSize: 1024,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
	}
}
