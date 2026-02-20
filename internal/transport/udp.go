package transport

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// UDPTransport implements Transport using UDP.
type UDPTransport struct {
	config  Config
	conn    *net.UDPConn
	addr    string

	handlers struct {
		message    MessageHandler
		connect    ConnectHandler
		disconnect DisconnectHandler
	}

	// Track known clients for connect/disconnect events
	clients   map[string]time.Time
	clientsMu sync.RWMutex

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewUDPTransport creates a new UDP transport.
func NewUDPTransport(config Config) *UDPTransport {
	return &UDPTransport{
		config: config,
		clients: make(map[string]time.Time),
		stopCh:  make(chan struct{}),
	}
}

// Listen starts listening on the given address.
func (t *UDPTransport) Listen(addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve udp addr: %w", err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}

	t.conn = conn
	t.addr = addr

	// Start receive loop
	t.wg.Add(1)
	go t.receiveLoop()

	return nil
}

// Close shuts down the transport.
func (t *UDPTransport) Close() error {
	close(t.stopCh)
	if t.conn != nil {
		t.conn.Close()
	}
	t.wg.Wait()
	return nil
}

// SendUnreliable sends data without guaranteed delivery.
func (t *UDPTransport) SendUnreliable(addr string, data []byte) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}

	_, err = t.conn.WriteToUDP(data, udpAddr)
	return err
}

// SendReliable sends data with guaranteed delivery.
// For UDP, this implements ACK/retry in a separate reliable sender.
func (t *UDPTransport) SendReliable(addr string, data []byte) error {
	// For Phase 1, just send unreliably
	// TODO: Implement ReliableSender in Phase 4
	return t.SendUnreliable(addr, data)
}

// OnMessage registers a handler for incoming messages.
func (t *UDPTransport) OnMessage(handler MessageHandler) {
	t.handlers.message = handler
}

// OnConnect registers a handler for new connections.
func (t *UDPTransport) OnConnect(handler ConnectHandler) {
	t.handlers.connect = handler
}

// OnDisconnect registers a handler for disconnections.
func (t *UDPTransport) OnDisconnect(handler DisconnectHandler) {
	t.handlers.disconnect = handler
}

// LocalAddr returns the local address.
func (t *UDPTransport) LocalAddr() string {
	if t.conn != nil {
		return t.conn.LocalAddr().String()
	}
	return t.addr
}

// receiveLoop handles incoming UDP packets.
func (t *UDPTransport) receiveLoop() {
	defer t.wg.Done()

	buf := make([]byte, t.config.MaxMessageSize)

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		n, addr, err := t.conn.ReadFromUDP(buf)
		if err != nil {
			// Check if we're shutting down
			select {
			case <-t.stopCh:
				return
			default:
				continue
			}
		}

		// Copy data (buf will be reused)
		data := make([]byte, n)
		copy(data, buf[:n])

		addrStr := addr.String()

		// Track client
		t.trackClient(addrStr)

		// Call message handler
		if t.handlers.message != nil {
			t.handlers.message(addrStr, data, false)
		}
	}
}

// trackClient tracks known clients for connect/disconnect events.
func (t *UDPTransport) trackClient(addr string) {
	t.clientsMu.Lock()
	defer t.clientsMu.Unlock()

	_, exists := t.clients[addr]
	t.clients[addr] = time.Now()

	// New client?
	if !exists && t.handlers.connect != nil {
		go t.handlers.connect(addr)
	}
}
