package webrtc

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Manager handles WebRTC peer connections for a room (SFU mode)
type Manager struct {
	mu            sync.RWMutex
	roomID        string
	peerConns     map[string]*webrtc.PeerConnection // playerID -> connection
	incomingTracks map[string]map[string]*webrtc.TrackRemote // playerID -> trackID -> track
	audioTracks   map[string]*webrtc.TrackLocalStaticRTP // playerID -> audio track to send
	videoTracks   map[string]*webrtc.TrackLocalStaticRTP // playerID -> video track to send
	trackChan     chan TrackEvent
	iceServers    []webrtc.ICEServer
}

// TrackEvent is sent when a track is received
type TrackEvent struct {
	PlayerID string
	Track    *webrtc.TrackRemote
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
		roomID:         roomID,
		peerConns:      make(map[string]*webrtc.PeerConnection),
		incomingTracks: make(map[string]map[string]*webrtc.TrackRemote),
		audioTracks:    make(map[string]*webrtc.TrackLocalStaticRTP),
		videoTracks:    make(map[string]*webrtc.TrackLocalStaticRTP),
		trackChan:      make(chan TrackEvent, 100),
		iceServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{URLs: []string{"stun:stun1.l.google.com:19302"}},
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

	// Initialize incoming tracks map for this player
	m.incomingTracks[playerID] = make(map[string]*webrtc.TrackRemote)

	// Handle incoming tracks - forward to all OTHER players
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("🎥 [%s] INCOMING %s track! Codec: %s, SSRC: %d", 
			playerID, track.Kind(), track.Codec().MimeType, track.SSRC())
		
		// Store incoming track
		m.mu.Lock()
		m.incomingTracks[playerID][track.ID()] = track
		m.mu.Unlock()

		// Forward RTP packets to all other players
		go m.forwardTrackToOthers(playerID, track)

		// Notify via channel
		select {
		case m.trackChan <- TrackEvent{
			PlayerID: playerID,
			Track:    track,
			RTP:      receiver,
		}:
		default:
		}
	})

	// Handle ICE candidates
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
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
	log.Printf("✅ [%s] Peer connection created, total: %d", playerID, len(m.peerConns))
	return pc, nil
}

// forwardTrackToOthers reads RTP from one player and forwards to all others
func (m *Manager) forwardTrackToOthers(fromPlayerID string, track *webrtc.TrackRemote) {
	var localTrack *webrtc.TrackLocalStaticRTP
	var err error

	codec := track.Codec()
	mimeType := codec.MimeType

	// Include clock rate for proper negotiation
	capability := webrtc.RTPCodecCapability{
		MimeType:  mimeType,
		ClockRate: codec.ClockRate,
		Channels:  codec.Channels,
	}

	// Create local track
	localTrack, err = webrtc.NewTrackLocalStaticRTP(
		capability,
		"track-"+fromPlayerID+"-"+string(track.Kind()),
		"stream-"+fromPlayerID,
	)
	if err != nil {
		log.Printf("❌ [%s] Failed to create local track: %v", fromPlayerID, err)
		return
	}

	// Store track
	m.mu.Lock()
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		m.audioTracks[fromPlayerID] = localTrack
	} else {
		m.videoTracks[fromPlayerID] = localTrack
	}
	log.Printf("📷 [%s] Created %s track (mime: %s, clock: %d)", fromPlayerID, track.Kind(), mimeType, codec.ClockRate)
	m.mu.Unlock()

	// Read and forward RTP packets
	rtpBuf := make([]byte, 1500)
	packets := 0
	for {
		n, attr, err := track.Read(rtpBuf)
		if err != nil {
			log.Printf("📭 [%s] Track read ended: %v", fromPlayerID, err)
			return
		}
		_ = attr

		if _, err := localTrack.Write(rtpBuf[:n]); err != nil {
			log.Printf("❌ [%s] RTP write error: %v", fromPlayerID, err)
			return
		}
		
		packets++
		if packets == 1 {
			log.Printf("📤 [%s] First %s packet forwarded!", fromPlayerID, track.Kind())
		}
		if packets % 100 == 0 {
			log.Printf("📤 [%s] Forwarded %d %s packets", fromPlayerID, packets, track.Kind())
		}
	}
}

// renegotiatePlayer creates a new offer for a player (when new tracks added)
func (m *Manager) renegotiatePlayer(playerID string) {
	m.mu.RLock()
	_, exists := m.peerConns[playerID]
	m.mu.RUnlock()

	if !exists {
		return
	}

	// For now, just log - full renegotiation requires offer/answer exchange via signaling
	log.Printf("🔄 Player %s needs renegotiation for new tracks", playerID)
}

// RemovePeerConnection removes a player's peer connection
func (m *Manager) RemovePeerConnection(playerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pc, exists := m.peerConns[playerID]; exists {
		pc.Close()
		delete(m.peerConns, playerID)
		delete(m.incomingTracks, playerID)
		delete(m.audioTracks, playerID)
		delete(m.videoTracks, playerID)
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

	// Add existing tracks from other players to this new player
	m.mu.RLock()
	audioCount := len(m.audioTracks)
	videoCount := len(m.videoTracks)
	log.Printf("🎥 [%s] Existing tracks: %d audio, %d video", playerID, audioCount, videoCount)

	for otherPlayerID, audioTrack := range m.audioTracks {
		if otherPlayerID != playerID {
			if _, err := pc.AddTrack(audioTrack); err != nil {
				log.Printf("❌ Failed to add audio from %s to %s: %v", otherPlayerID, playerID, err)
			} else {
				log.Printf("🎵 Added audio from %s to new player %s", otherPlayerID, playerID)
			}
		}
	}
	for otherPlayerID, videoTrack := range m.videoTracks {
		if otherPlayerID != playerID {
			if _, err := pc.AddTrack(videoTrack); err != nil {
				log.Printf("❌ Failed to add video from %s to %s: %v", otherPlayerID, playerID, err)
			} else {
				log.Printf("📹 Added video from %s to new player %s", otherPlayerID, playerID)
			}
		}
	}
	m.mu.RUnlock()

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("❌ Failed to create answer: %v", err)
		return nil, err
	}

	// Set local description
	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, err
	}

	log.Printf("✅ [%s] Created answer with %d senders", playerID, len(pc.GetSenders()))
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

// GetTrackEvents returns the channel for track events
func (m *Manager) GetTrackEvents() <-chan TrackEvent {
	return m.trackChan
}

// Close closes all peer connections
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, pc := range m.peerConns {
		pc.Close()
	}
	m.peerConns = make(map[string]*webrtc.PeerConnection)
	close(m.trackChan)
}

// GenerateClientID generates a unique ID for WebRTC
func GenerateClientID() string {
	return uuid.New().String()[:8]
}