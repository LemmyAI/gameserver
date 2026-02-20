package transport

import (
	"sync"
)

// MockTransport is a mock implementation for testing.
type MockTransport struct {
	addr     string
	messages []MockMessage
	sent     []MockMessage
	mu       sync.Mutex
	handlers struct {
		message    MessageHandler
		connect    ConnectHandler
		disconnect DisconnectHandler
	}
}

// MockMessage records a sent or received message.
type MockMessage struct {
	Addr     string
	Data     []byte
	Reliable bool
}

// NewMockTransport creates a new mock transport.
func NewMockTransport() *MockTransport {
	return &MockTransport{
		messages: make([]MockMessage, 0),
		sent:     make([]MockMessage, 0),
	}
}

// Listen does nothing in mock.
func (t *MockTransport) Listen(addr string) error {
	t.addr = addr
	return nil
}

// Close does nothing in mock.
func (t *MockTransport) Close() error {
	return nil
}

// SendUnreliable records the message as sent.
func (t *MockTransport) SendUnreliable(addr string, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sent = append(t.sent, MockMessage{Addr: addr, Data: data, Reliable: false})
	return nil
}

// SendReliable records the message as sent reliably.
func (t *MockTransport) SendReliable(addr string, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sent = append(t.sent, MockMessage{Addr: addr, Data: data, Reliable: true})
	return nil
}

// OnMessage registers a handler.
func (t *MockTransport) OnMessage(handler MessageHandler) {
	t.handlers.message = handler
}

// OnConnect registers a handler.
func (t *MockTransport) OnConnect(handler ConnectHandler) {
	t.handlers.connect = handler
}

// OnDisconnect registers a handler.
func (t *MockTransport) OnDisconnect(handler DisconnectHandler) {
	t.handlers.disconnect = handler
}

// LocalAddr returns the mock address.
func (t *MockTransport) LocalAddr() string {
	return t.addr
}

// --- Test helpers ---

// SimulateMessage simulates receiving a message.
func (t *MockTransport) SimulateMessage(addr string, data []byte, reliable bool) {
	t.mu.Lock()
	t.messages = append(t.messages, MockMessage{Addr: addr, Data: data, Reliable: reliable})
	t.mu.Unlock()

	if t.handlers.message != nil {
		t.handlers.message(addr, data, reliable)
	}
}

// SimulateConnect simulates a client connecting.
func (t *MockTransport) SimulateConnect(addr string) {
	if t.handlers.connect != nil {
		t.handlers.connect(addr)
	}
}

// SimulateDisconnect simulates a client disconnecting.
func (t *MockTransport) SimulateDisconnect(addr string) {
	if t.handlers.disconnect != nil {
		t.handlers.disconnect(addr)
	}
}

// SentMessages returns all sent messages.
func (t *MockTransport) SentMessages() []MockMessage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]MockMessage{}, t.sent...)
}

// ReceivedMessages returns all received messages.
func (t *MockTransport) ReceivedMessages() []MockMessage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]MockMessage{}, t.messages...)
}

// Clear clears all recorded messages.
func (t *MockTransport) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages = t.messages[:0]
	t.sent = t.sent[:0]
}
