// Game client with native WebRTC video

// Get room ID from URL: /room/{roomId}
const pathParts = window.location.pathname.split('/');
const ROOM_ID = pathParts[2] || null;
const PLAYER_NAME = 'Player' + Math.floor(Math.random() * 1000);

console.log('🎮 Room ID:', ROOM_ID, 'Player name:', PLAYER_NAME);

// WebSocket connection
let ws = null;
let myId = null;
let players = {};
let myPlayer = { x: 500, y: 500, vx: 0, vy: 0 };
let keys = { up: false, down: false, left: false, right: false };

// Dynamic host detection
const HOST = window.location.host;
const WS_PROTOCOL = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const HTTP_PROTOCOL = window.location.protocol;

// WebRTC
let peerConnection = null;
let localStream = null;
let micEnabled = true;
let camEnabled = true;
let webrtcConnected = false;

// ICE servers (STUN)
const iceServers = [
    { urls: 'stun:stun.l.google.com:19302' },
    { urls: 'stun:stun1.l.google.com:19302' }
];

// Canvas setup
const canvas = document.getElementById('game');
const ctx = canvas.getContext('2d');

function resizeCanvas() {
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;
}
resizeCanvas();
window.addEventListener('resize', resizeCanvas);

// ================== WebRTC ==================

async function connectWebRTC() {
    console.log('🎥 connectWebRTC called - ROOM_ID:', ROOM_ID, 'myId:', myId, 'connected:', webrtcConnected);

    if (!ROOM_ID || !myId) {
        console.log('⚠️ No room or player ID yet, skipping WebRTC');
        return;
    }

    if (webrtcConnected) {
        console.log('🎥 WebRTC already connected');
        return;
    }

    webrtcConnected = true;
    console.log('🎥 Starting WebRTC connection...');

    try {
        // Create peer connection
        peerConnection = new RTCPeerConnection({ iceServers });

        // Handle ICE candidates
        peerConnection.onicecandidate = (event) => {
            if (event.candidate) {
                console.log('🧊 ICE candidate:', event.candidate.type);
                ws.send(JSON.stringify({
                    type: 'webrtc_ice',
                    roomId: ROOM_ID,
                    playerId: myId,
                    candidate: event.candidate.toJSON()
                }));
            }
        };

        // Handle connection state changes
        peerConnection.onconnectionstatechange = () => {
            console.log('🎥 Connection state:', peerConnection.connectionState);
            if (peerConnection.connectionState === 'connected') {
                showToast('Video connected!');
            } else if (peerConnection.connectionState === 'disconnected' ||
                       peerConnection.connectionState === 'failed') {
                showToast('Video disconnected');
                webrtcConnected = false;
            }
        };

        // Handle incoming tracks
        peerConnection.ontrack = (event) => {
            console.log('📺 Received track:', event.track.kind);
            const stream = event.streams[0];
            if (stream) {
                addRemoteStream(stream);
            }
        };

        // Get local media (try separately, don't require both)
        let hasAudio = false, hasVideo = false;
        
        // Try audio first
        try {
            const audioStream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
            audioStream.getTracks().forEach(track => {
                peerConnection.addTrack(track, new MediaStream([track]));
                hasAudio = true;
            });
            console.log('🎤 Audio track added');
        } catch (e) {
            console.log('🎤 No audio available:', e.message);
        }
        
        // Try video separately
        try {
            const videoStream = await navigator.mediaDevices.getUserMedia({ audio: false, video: true });
            videoStream.getTracks().forEach(track => {
                peerConnection.addTrack(track, new MediaStream([track]));
                hasVideo = true;
            });
            console.log('📷 Video track added');
        } catch (e) {
            console.log('📷 No video available:', e.message);
        }

        // Create local stream for display (if we have any tracks)
        if (hasAudio || hasVideo) {
            const tracks = peerConnection.getSenders().map(s => s.track).filter(t => t);
            localStream = new MediaStream(tracks);
        }
        
        // Show self in grid (with or without video)
        addSelfToGrid(localStream, hasVideo);
        
        if (!hasAudio && !hasVideo) {
            showToast('No camera/mic - you can still see and hear others!');
        }

        // Create offer
        const offer = await peerConnection.createOffer();
        await peerConnection.setLocalDescription(offer);
        
        console.log('📤 Sending WebRTC offer');
        ws.send(JSON.stringify({
            type: 'webrtc_offer',
            roomId: ROOM_ID,
            playerId: myId,
            sdp: offer.sdp
        }));

    } catch (err) {
        console.error('WebRTC connection error:', err);
        showToast('Video connection failed');
        webrtcConnected = false;
    }
}

function addSelfToGrid(stream, hasVideo = true) {
    const grid = document.getElementById('video-grid');
    
    // Remove if exists
    const existing = document.getElementById(`video-${myId}`);
    if (existing) existing.remove();
    
    const div = document.createElement('div');
    div.className = 'video-tile self';
    div.id = `video-${myId}`;

    if (stream && hasVideo) {
        const video = document.createElement('video');
        video.autoplay = true;
        video.muted = true; // Mute self to prevent feedback
        video.playsInline = true;
        video.srcObject = stream;
        div.appendChild(video);
    } else {
        // Placeholder when no camera
        const placeholder = document.createElement('div');
        placeholder.className = 'video-placeholder';
        placeholder.innerHTML = '🎤<br><small>' + (stream ? 'Audio only' : 'No media') + '</small>';
        placeholder.style.cssText = 'display:flex;align-items:center;justify-content:center;flex-direction:column;color:#888;font-size:24px;height:100%;background:#1a1a2e;';
        div.appendChild(placeholder);
    }

    const label = document.createElement('span');
    label.className = 'video-label';
    label.textContent = 'You';

    div.appendChild(label);
    grid.appendChild(div);
}

function addRemoteStream(stream, participantId) {
    const grid = document.getElementById('video-grid');
    
    // Remove if exists
    const existing = document.getElementById(`video-remote-${participantId || 'unknown'}`);
    if (existing) existing.remove();
    
    const div = document.createElement('div');
    div.className = 'video-tile';
    div.id = `video-remote-${participantId || Date.now()}`;

    const video = document.createElement('video');
    video.autoplay = true;
    video.playsInline = true;
    video.srcObject = stream;

    const label = document.createElement('span');
    label.className = 'video-label';
    label.textContent = 'Player';

    div.appendChild(video);
    div.appendChild(label);
    grid.appendChild(div);
    
    showNotification(`🎥 Player joined video`);
}

async function handleWebRTCAnswer(data) {
    if (!peerConnection) {
        console.warn('No peer connection for answer');
        return;
    }
    
    console.log('📥 Received WebRTC answer');
    try {
        await peerConnection.setRemoteDescription({
            type: 'answer',
            sdp: data.sdp
        });
        console.log('✅ Remote description set');
    } catch (err) {
        console.error('Failed to set remote description:', err);
    }
}

async function handleWebRTCIce(data) {
    if (!peerConnection) {
        console.warn('No peer connection for ICE candidate');
        return;
    }
    
    console.log('📥 Received ICE candidate');
    try {
        await peerConnection.addIceCandidate(new RTCIceCandidate(data.candidate));
    } catch (err) {
        console.error('Failed to add ICE candidate:', err);
    }
}

function toggleMic() {
    micEnabled = !micEnabled;
    document.getElementById('btn-mic').textContent = micEnabled ? '🎤' : '🔇';
    
    if (localStream) {
        localStream.getAudioTracks().forEach(track => {
            track.enabled = micEnabled;
        });
    }
    showToast(micEnabled ? 'Microphone on' : 'Microphone off');
}

function toggleCam() {
    camEnabled = !camEnabled;
    document.getElementById('btn-cam').textContent = camEnabled ? '📷' : '📹';
    
    if (localStream) {
        localStream.getVideoTracks().forEach(track => {
            track.enabled = camEnabled;
        });
    }
    showToast(camEnabled ? 'Camera on' : 'Camera off');
}

// ================== Game Logic ==================

function connect() {
    const wsUrl = `${WS_PROTOCOL}//${HOST}/ws`;
    console.log('🔌 Connecting to:', wsUrl);
    
    ws = new WebSocket(wsUrl);
    
    ws.onopen = () => {
        console.log('🔌 Connected to server');
    };
    
    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            handleMessage(data);
        } catch (e) {
            console.error('Parse error:', e);
        }
    };
    
    ws.onclose = () => {
        console.log('🔌 Disconnected from server');
        showToast('Disconnected from server');
    };
    
    ws.onerror = (err) => {
        console.error('WebSocket error:', err);
    };
}

function handleMessage(data) {
    // Log received messages (except state updates which are frequent)
    if (data.type !== 'state') {
        console.log('📨 Received:', data.type, data);
    }
    
    switch (data.type) {
        case 'welcome':
            myId = data.id;
            console.log('✅ Got player ID from welcome:', myId);
            document.getElementById('player-id').textContent = myId;
            
            // Join room
            ws.send(JSON.stringify({
                type: 'join_room',
                roomId: ROOM_ID,
                name: PLAYER_NAME
            }));
            break;
            
        case 'room_joined':
            console.log('✅ Joined room:', data.roomId, 'myId:', myId);
            document.getElementById('room-id').textContent = data.roomId;
            document.getElementById('player-count').textContent = data.playerCount;
            
            // Connect WebRTC after joining room
            connectWebRTC();
            break;
            
        case 'player_joined':
            showNotification(`👋 ${data.playerName} joined (${data.playerCount} players)`);
            document.getElementById('player-count').textContent = data.playerCount;
            break;
            
        case 'player_left':
            showNotification(`👋 ${data.playerName} left`);
            document.getElementById('player-count').textContent = data.playerCount || '?';
            break;
            
        case 'state':
            // Update player positions
            if (data.players) {
                data.players.forEach(p => {
                    players[p.id] = p;
                    if (p.id === myId) {
                        myPlayer.x = p.x;
                        myPlayer.y = p.y;
                    }
                });
            }
            break;
            
        case 'webrtc_answer':
            handleWebRTCAnswer(data);
            break;
            
        case 'webrtc_ice':
            handleWebRTCIce(data);
            break;
            
        case 'webrtc_offer':
            // For now, we're the initiator (caller)
            // In a full mesh, we'd handle incoming offers too
            break;
            
        case 'error':
            showToast('Error: ' + data.error);
            break;
    }
}

// ================== Game Loop ==================

function gameLoop() {
    // Clear
    ctx.fillStyle = '#0a0a0f';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    
    // Draw grid
    ctx.strokeStyle = '#1a1a2e';
    ctx.lineWidth = 1;
    for (let x = 0; x < canvas.width; x += 50) {
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, canvas.height);
        ctx.stroke();
    }
    for (let y = 0; y < canvas.height; y += 50) {
        ctx.beginPath();
        ctx.moveTo(0, y);
        ctx.lineTo(canvas.width, y);
        ctx.stroke();
    }
    
    // Draw players
    Object.values(players).forEach(p => {
        const isMe = p.id === myId;
        
        // Draw glow
        const gradient = ctx.createRadialGradient(p.x, p.y, 10, p.x, p.y, 30);
        gradient.addColorStop(0, isMe ? 'rgba(0, 212, 255, 0.5)' : 'rgba(124, 58, 237, 0.3)');
        gradient.addColorStop(1, 'transparent');
        ctx.fillStyle = gradient;
        ctx.beginPath();
        ctx.arc(p.x, p.y, 30, 0, Math.PI * 2);
        ctx.fill();
        
        // Draw player
        ctx.beginPath();
        ctx.arc(p.x, p.y, 15, 0, Math.PI * 2);
        ctx.fillStyle = isMe ? '#00d4ff' : '#7c3aed';
        ctx.fill();
        ctx.strokeStyle = isMe ? '#00ffff' : '#9b59b6';
        ctx.lineWidth = 2;
        ctx.stroke();
        
        // Draw name
        ctx.fillStyle = '#fff';
        ctx.font = '12px Inter, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText(isMe ? 'You' : p.id.slice(0, 4), p.x, p.y - 25);
    });
    
    requestAnimationFrame(gameLoop);
}

// ================== Input ==================

document.addEventListener('keydown', (e) => {
    switch (e.key) {
        case 'w': case 'W': case 'ArrowUp': keys.up = true; break;
        case 's': case 'S': case 'ArrowDown': keys.down = true; break;
        case 'a': case 'A': case 'ArrowLeft': keys.left = true; break;
        case 'd': case 'D': case 'ArrowRight': keys.right = true; break;
    }
});

document.addEventListener('keyup', (e) => {
    switch (e.key) {
        case 'w': case 'W': case 'ArrowUp': keys.up = false; break;
        case 's': case 'S': case 'ArrowDown': keys.down = false; break;
        case 'a': case 'A': case 'ArrowLeft': keys.left = false; break;
        case 'd': case 'D': case 'ArrowRight': keys.right = false; break;
    }
});

// Send input to server
setInterval(() => {
    if (ws && ws.readyState === WebSocket.OPEN && myId) {
        const dx = (keys.right ? 1 : 0) - (keys.left ? 1 : 0);
        const dy = (keys.down ? 1 : 0) - (keys.up ? 1 : 0);
        
        if (dx !== 0 || dy !== 0) {
            ws.send(JSON.stringify({
                type: 'input',
                dx: dx,
                dy: dy
            }));
        }
    }
}, 1000 / 60); // 60 Hz

// ================== UI ==================

function showToast(message) {
    const toast = document.createElement('div');
    toast.className = 'toast';
    toast.textContent = message;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 2000);
}

function showNotification(message) {
    if ('Notification' in window && Notification.permission === 'granted') {
        new Notification('GameServer', { body: message });
    }
}

// Copy share link
document.getElementById('share-link').value = window.location.href;
function copyLink() {
    navigator.clipboard.writeText(window.location.href);
    showToast('Link copied!');
}

// Make functions global
window.toggleMic = toggleMic;
window.toggleCam = toggleCam;
window.copyLink = copyLink;

// Start
connect();
gameLoop();