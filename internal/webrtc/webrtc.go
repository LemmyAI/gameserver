package webrtc

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// RenegotiateEvent is sent when a new track needs to be sent to existing players
type RenegotiateEvent struct {
	PlayerID string
	Track    *webrtc.TrackLocalStaticRTP
	Kind     webrtc.RTPCodecType
}

// Manager handles WebRTC peer connections for a room (SFU mode)
type Manager struct {
	mu             sync.RWMutex
	roomID         string
	peerConns      map[string]*webrtc.PeerConnection // playerID -> connection
	incomingTracks map[string]map[string]*webrtc.TrackRemote
	audioTracks    map[string]*webrtc.TrackLocalStaticRTP
	videoTracks    map[string]*webrtc.TrackLocalStaticRTP
	trackChan      chan TrackEvent
	renegotiateChan chan RenegotiateEvent
	iceServers     []webrtc.ICEServer
}

// TrackEvent is sent when a track is received
type TrackEvent struct {
	PlayerID string
	Track    *webrtc.TrackRemote
	RTP      *webrtc.RTPReceiver
}

// SignalMessage is sent over WebSocket for signaling
type SignalMessage struct {
	Type      string          `json:"type"`
	PlayerID  string          `json:"playerId"`
	RoomID    string          `json:"roomId"`
	SDP       string          `json:"sdp"`
	Candidate json.RawMessage `json:"candidate"`
}

// NewManager creates a new WebRTC manager for a room
func NewManager(roomID string) *Manager {
	return &Manager{
		roomID:          roomID,
		peerConns:       make(map[string]*webrtc.PeerConnection),
		incomingTracks:  make(map[string]map[string]*webrtc.TrackRemote),
		audioTracks:     make(map[string]*webrtc.TrackLocalStaticRTP),
		videoTracks:     make(map[string]*webrtc.TrackLocalStaticRTP),
		trackChan:       make(chan TrackEvent, 100),
		renegotiateChan: make(chan RenegotiateEvent, 100),
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

	m.incomingTracks[playerID] = make(map[string]*webrtc.TrackRemote)

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("🎥 [%s] INCOMING %s track! Codec: %s", playerID, track.Kind(), track.Codec().MimeType)
		
		m.mu.Lock()
		m.incomingTracks[playerID][track.ID()] = track
		m.mu.Unlock()

		go m.forwardTrackToOthers(playerID, track)

		select {
		case m.trackChan <- TrackEvent{PlayerID: playerID, Track: track, RTP: receiver}:
		default:
		}
	})

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
	})

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
	codec := track.Codec()
	
	capability := webrtc.RTPCodecCapability{
		MimeType:  codec.MimeType,
		ClockRate: codec.ClockRate,
		Channels:  codec.Channels,
	}

	localTrack, err := webrtc.NewTrackLocalStaticRTP(
		capability,
		"track-"+fromPlayerID+"-"+string(track.Kind()),
		"stream-"+fromPlayerID,
	)
	if err != nil {
		log.Printf("❌ [%s] Failed to create local track: %v", fromPlayerID, err)
		return
	}

	m.mu.Lock()
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		m.audioTracks[fromPlayerID] = localTrack
	} else {
		m.videoTracks[fromPlayerID] = localTrack
	}
	
	// Add track to all OTHER players and prepare renegotiation
	var toRenegotiate []string
	for playerID, pc := range m.peerConns {
		if playerID != fromPlayerID {
			if _, err := pc.AddTrack(localTrack); err != nil {
				log.Printf("❌ [FORWARD] Failed to add track to %s: %v", playerID, err)
			} else {
				log.Printf("✅ [FORWARD] Added %s track from %s to %s", track.Kind(), fromPlayerID, playerID)
				toRenegotiate = append(toRenegotiate, playerID)
			}
		}
	}
	m.mu.Unlock()

	log.Printf("📷 [%s] Created %s track, need to renegotiate with: %v", fromPlayerID, track.Kind(), toRenegotiate)

	// Queue renegotiation events
	for _, playerID := range toRenegotiate {
		select {
		case m.renegotiateChan <- RenegotiateEvent{PlayerID: playerID, Track: localTrack, Kind: track.Kind()}:
			log.Printf("🔄 [FORWARD] Queued renegotiation event for %s", playerID)
		default:
			log.Printf("⚠️ [FORWARD] Renegotiate channel full, dropping event for %s", playerID)
		}
	}

	// Forward RTP packets
	rtpBuf := make([]byte, 1500)
	packets := 0
	for {
		n, _, err := track.Read(rtpBuf)
		if err != nil {
			log.Printf("📭 [%s] Track ended: %v", fromPlayerID, err)
			return
		}

		if _, err := localTrack.Write(rtpBuf[:n]); err != nil {
			log.Printf("❌ [%s] RTP write error: %v", fromPlayerID, err)
			return
		}
		
		packets++
		if packets == 1 {
			log.Printf("📤 [%s] First %s RTP packet forwarded!", fromPlayerID, track.Kind())
		}
	}
}

// GetRenegotiateChan returns the channel for renegotiation events
func (m *Manager) GetRenegotiateChan() <-chan RenegotiateEvent {
	return m.renegotiateChan
}

// CreateOffer creates a new offer for a player (for renegotiation)
func (m *Manager) CreateOffer(playerID string) (*webrtc.SessionDescription, error) {
	m.mu.RLock()
	pc, exists := m.peerConns[playerID]
	m.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, err
	}

	if err := pc.SetLocalDescription(offer); err != nil {
		return nil, err
	}

	log.Printf("📤 [%s] Created renegotiation offer", playerID)
	return &offer, nil
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

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, err
	}

	// Add existing tracks from other players
	m.mu.RLock()
	log.Printf("🎥 [%s] Existing tracks: %d audio, %d video", playerID, len(m.audioTracks), len(m.videoTracks))
	for otherPlayerID, audioTrack := range m.audioTracks {
		if otherPlayerID != playerID {
			if _, err := pc.AddTrack(audioTrack); err != nil {
				log.Printf("❌ Failed to add audio from %s: %v", otherPlayerID, err)
			} else {
				log.Printf("🎵 Added audio from %s", otherPlayerID)
			}
		}
	}
	for otherPlayerID, videoTrack := range m.videoTracks {
		if otherPlayerID != playerID {
			if _, err := pc.AddTrack(videoTrack); err != nil {
				log.Printf("❌ Failed to add video from %s: %v", otherPlayerID, err)
			} else {
				log.Printf("📹 Added video from %s", otherPlayerID)
			}
		}
	}
	m.mu.RUnlock()

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, err
	}

	log.Printf("✅ [%s] Answer created, senders: %d", playerID, len(pc.GetSenders()))
	return &answer, nil
}

// HandleAnswer handles an SDP answer from a client
func (m *Manager) HandleAnswer(playerID string, sdp string) error {
	m.mu.RLock()
	pc, exists := m.peerConns[playerID]
	m.mu.RUnlock()

	if !exists {
		return nil
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

// GetSenders returns the number of senders for a player
func (m *Manager) GetSenders(playerID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if pc, exists := m.peerConns[playerID]; exists {
		return len(pc.GetSenders())
	}
	return 0
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
	close(m.renegotiateChan)
}

// GenerateClientID generates a unique ID for WebRTC
func GenerateClientID() string {
	return uuid.New().String()[:8]
}