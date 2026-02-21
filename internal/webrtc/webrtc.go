package webrtc

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Manager handles WebRTC peer connections for a room
type Manager struct {
	mu           sync.RWMutex
	roomID       string
	peerConns    map[string]*webrtc.PeerConnection // playerID -> connection
	localTracks  map[string][]webrtc.TrackLocal    // playerID -> tracks
	trackChan    chan TrackEvent
	iceServers   []webrtc.ICEServer
}

// TrackEvent is sent when a track is received
type TrackEvent struct {
	PlayerID string
	Track    webrtc.TrackRemote
	RTP      *webrtc.RTPReceiver
}

// SignalMessage is sent over WebSocket for signaling
type SignalMessage struct {
	Type      string          `json:"type"`      // "offer", "answer", "ice-candidate"
	PlayerID  string          `json:"playerId"`  // Who this is from/to
	RoomID    string          `json:"roomId"`
	SDP       string          `json:"sdp"`       // For offer/answer
	Candidate json.RawMessage `json:"candidate"` // For ICE candidate
}

// NewManager creates a new WebRTC manager for a room
func NewManager(roomID string) *Manager {
	return &Manager{
		roomID:      roomID,
		peerConns:   make(map[string]*webrtc.PeerConnection),
		localTracks: make(map[string][]webrtc.TrackLocal),
		trackChan:   make(chan TrackEvent, 100),
		iceServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
}

// CreatePeerConnection creates a new peer connection for a player
func (m *Manager) CreatePeerConnection(playerID string) (*webrtc.PeerConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if pc, exists := m.peerConns[playerID]; exists {
		return pc, nil
	}

	config := webrtc.Configuration{
		ICEServers: m.iceServers,
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	// Handle incoming tracks
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("🎥 [%s] Received track: %s", playerID, track.Kind())
		
		// Broadcast to other players
		select {
		case m.trackChan <- TrackEvent{
			PlayerID: playerID,
			Track:    *track,
			RTP:      receiver,
		}:
		default:
			log.Printf("Track channel full, dropping track event")
		}
	})

	// Handle ICE candidates
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		// ICE candidates are sent via the signaling channel (WebSocket)
		// The caller will handle this
	})

	// Handle connection state
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("🎥 [%s] Connection state: %s", playerID, state)
		if state == webrtc.PeerConnectionStateDisconnected ||
			state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed {
			m.RemovePeerConnection(playerID)
		}
	})

	m.peerConns[playerID] = pc
	return pc, nil
}

// RemovePeerConnection removes a player's peer connection
func (m *Manager) RemovePeerConnection(playerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pc, exists := m.peerConns[playerID]; exists {
		pc.Close()
		delete(m.peerConns, playerID)
		delete(m.localTracks, playerID)
	}
}

// HandleOffer handles an SDP offer from a client
func (m *Manager) HandleOffer(playerID string, sdp string) (*webrtc.SessionDescription, error) {
	pc, err := m.CreatePeerConnection(playerID)
	if err != nil {
		return nil, err
	}

	// Set remote description (the offer)
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, err
	}

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}

	// Set local description
	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, err
	}

	return &answer, nil
}

// HandleAnswer handles an SDP answer from a client
func (m *Manager) HandleAnswer(playerID string, sdp string) error {
	m.mu.RLock()
	pc, exists := m.peerConns[playerID]
	m.mu.RUnlock()

	if !exists {
		return nil // Player might have disconnected
	}

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	}
	return pc.SetRemoteDescription(answer)
}

// HandleICECandidate handles an ICE candidate from a client
func (m *Manager) HandleICECandidate(playerID string, candidate json.RawMessage) error {
	m.mu.RLock()
	pc, exists := m.peerConns[playerID]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	var iceCandidate webrtc.ICECandidateInit
	if err := json.Unmarshal(candidate, &iceCandidate); err != nil {
		return err
	}

	return pc.AddICECandidate(iceCandidate)
}

// GetICECandidates returns pending ICE candidates for a player (simplified)
func (m *Manager) GetICECandidates(playerID string) ([]map[string]interface{}, error) {
	m.mu.RLock()
	pc, exists := m.peerConns[playerID]
	m.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	// For now, return empty - ICE candidates are exchanged via OnICECandidate callback
	// The client will receive candidates through the signaling channel
	_ = pc
	return []map[string]interface{}{}, nil
}

// BroadcastTrack sends a track to all other players in the room
func (m *Manager) BroadcastTrack(fromPlayerID string, track webrtc.TrackLocal) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for playerID, pc := range m.peerConns {
		if playerID == fromPlayerID {
			continue // Don't send to self
		}

		_, err := pc.AddTrack(track)
		if err != nil {
			log.Printf("❌ Failed to add track to %s: %v", playerID, err)
		}
	}
}

// GetTrackEvents returns the channel for track events
func (m *Manager) GetTrackEvents() <-chan TrackEvent {
	return m.trackChan
}

// Close closes all peer connections
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for playerID, pc := range m.peerConns {
		pc.Close()
		delete(m.peerConns, playerID)
	}
	close(m.trackChan)
}

// GenerateClientID generates a unique ID for WebRTC
func GenerateClientID() string {
	return uuid.New().String()[:8]
}