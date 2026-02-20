// Game client with room support and LiveKit video

// Get room ID from URL: /room/{roomId}
const pathParts = window.location.pathname.split('/');
const ROOM_ID = pathParts[2] || null;
const PLAYER_NAME = 'Player' + Math.floor(Math.random() * 1000);

// WebSocket connection
let ws = null;
let myId = null;
let players = {};
let myPlayer = { x: 500, y: 500, vx: 0, vy: 0 };
let keys = { up: false, down: false, left: false, right: false };

// LiveKit
let room = null;
let micEnabled = true;
let camEnabled = true;

// Canvas setup
const canvas = document.getElementById('game');
const ctx = canvas.getContext('2d');

function resizeCanvas() {
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;
}
resizeCanvas();
window.addEventListener('resize', resizeCanvas);

// ================== LiveKit ==================

async function connectLiveKit() {
    if (!ROOM_ID || !myId) {
        console.log('No room or player ID yet, skipping LiveKit');
        return;
    }

    try {
        // Get token from server
        const res = await fetch('/livekit/token', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                roomId: ROOM_ID,
                playerId: myId,
                playerName: PLAYER_NAME
            })
        });

        if (!res.ok) {
            console.error('Failed to get LiveKit token');
            return;
        }

        const { token, url } = await res.json();
        console.log('🎥 Connecting to LiveKit:', url);

        // Connect to LiveKit room
        room = new LivekitClient.Room({
            adaptiveStream: true,
            dynacast: true,
        });

        // Handle participants
        room.on(LivekitClient.RoomEvent.ParticipantConnected, (participant) => {
            console.log('👋 Participant joined:', participant.identity);
            addVideoTrack(participant);
        });

        room.on(LivekitClient.RoomEvent.ParticipantDisconnected, (participant) => {
            console.log('👋 Participant left:', participant.identity);
            removeVideoTrack(participant.identity);
        });

        room.on(LivekitClient.RoomEvent.TrackSubscribed, (track, publication, participant) => {
            console.log('📺 Track subscribed:', track.kind, 'from', participant.identity);
            if (track.kind === 'video' || track.kind === 'audio') {
                addVideoTrack(participant, track);
            }
        });

        room.on(LivekitClient.RoomEvent.TrackUnsubscribed, (track, publication, participant) => {
            console.log('📺 Track unsubscribed:', track.kind);
            if (track.kind === 'video') {
                removeVideoTrack(participant.identity);
            }
        });

        // Connect
        await room.connect(url, token);
        console.log('✅ Connected to LiveKit room:', room.name);

        // Publish local tracks
        await publishLocalTracks();

        // Add existing participants
        room.participants.forEach((participant) => {
            addVideoTrack(participant);
        });

        // Add self to video grid
        addSelfToGrid();

    } catch (err) {
        console.error('LiveKit connection error:', err);
        showToast('Video connection failed');
    }
}

async function publishLocalTracks() {
    try {
        // Publish microphone
        const audioTrack = await LivekitClient.createLocalAudioTrack();
        await room.localParticipant.publishTrack(audioTrack);
        console.log('🎤 Published audio track');

        // Publish camera
        const videoTrack = await LivekitClient.createLocalVideoTrack();
        await room.localParticipant.publishTrack(videoTrack);
        console.log('📷 Published video track');

    } catch (err) {
        console.error('Failed to publish tracks:', err);
        // Try just audio if video fails
        try {
            const audioTrack = await LivekitClient.createLocalAudioTrack();
            await room.localParticipant.publishTrack(audioTrack);
        } catch (e) {
            console.error('Audio also failed:', e);
        }
    }
}

function addSelfToGrid() {
    const grid = document.getElementById('video-grid');
    const div = document.createElement('div');
    div.className = 'video-tile self';
    div.id = `video-${myId}`;

    const video = document.createElement('video');
    video.autoplay = true;
    video.muted = true; // Mute self
    video.playsInline = true;

    // Attach local video
    if (room && room.localParticipant.videoTrackPublications.size > 0) {
        room.localParticipant.videoTrackPublications.forEach((pub) => {
            if (pub.track) {
                video.srcObject = new MediaStream([pub.track.mediaStreamTrack]);
            }
        });
    }

    const label = document.createElement('span');
    label.className = 'video-label';
    label.textContent = 'You';

    div.appendChild(video);
    div.appendChild(label);
    grid.appendChild(div);
}

function addVideoTrack(participant, track = null) {
    const grid = document.getElementById('video-grid');
    let div = document.getElementById(`video-${participant.identity}`);

    if (!div) {
        div = document.createElement('div');
        div.className = 'video-tile';
        div.id = `video-${participant.identity}`;

        const video = document.createElement('video');
        video.autoplay = true;
        video.playsInline = true;
        video.id = `video-el-${participant.identity}`;
        div.appendChild(video);

        const label = document.createElement('span');
        label.className = 'video-label';
        label.textContent = participant.name || participant.identity;
        div.appendChild(label);

        grid.appendChild(div);
    }

    // Attach track if provided
    if (track && (track.kind === 'video' || track.kind === 'audio')) {
        const video = document.getElementById(`video-el-${participant.identity}`);
        if (video) {
            if (track.kind === 'video') {
                video.srcObject = new MediaStream([track.mediaStreamTrack]);
            } else if (track.kind === 'audio') {
                // Add audio to existing video element
                const stream = video.srcObject || new MediaStream();
                stream.addTrack(track.mediaStreamTrack);
                video.srcObject = stream;
            }
        }
    } else {
        // Find any video/audio tracks for this participant
        participant.videoTrackPublications?.forEach((pub) => {
            if (pub.track) {
                const video = document.getElementById(`video-el-${participant.identity}`);
                if (video) {
                    const stream = video.srcObject || new MediaStream();
                    stream.addTrack(pub.track.mediaStreamTrack);
                    video.srcObject = stream;
                }
            }
        });
        participant.audioTrackPublications?.forEach((pub) => {
            if (pub.track) {
                const video = document.getElementById(`video-el-${participant.identity}`);
                if (video) {
                    const stream = video.srcObject || new MediaStream();
                    stream.addTrack(pub.track.mediaStreamTrack);
                    video.srcObject = stream;
                }
            }
        });
    }

    updatePlayerCount();
}

function removeVideoTrack(identity) {
    const div = document.getElementById(`video-${identity}`);
    if (div) {
        div.remove();
    }
    updatePlayerCount();
}

function updatePlayerCount() {
    const grid = document.getElementById('video-grid');
    const count = grid.children.length;
    document.getElementById('player-count').textContent = count;
}

async function toggleMic() {
    if (!room) return;

    micEnabled = !micEnabled;
    const btn = document.getElementById('btn-mic');
    btn.textContent = micEnabled ? '🎤' : '🔇';
    btn.classList.toggle('disabled', !micEnabled);

    await room.localParticipant.setMicrophoneEnabled(micEnabled);
}

async function toggleCam() {
    if (!room) return;

    camEnabled = !camEnabled;
    const btn = document.getElementById('btn-cam');
    btn.textContent = camEnabled ? '📷' : '📵';
    btn.classList.toggle('disabled', !camEnabled);

    await room.localParticipant.setCameraEnabled(camEnabled);
}

// ================== WebSocket ==================

function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(`${protocol}//${window.location.host}/ws`);

    ws.onopen = () => {
        console.log('🔌 Connected to server');

        // If we have a room ID, join the room
        if (ROOM_ID) {
            ws.send(JSON.stringify({
                type: 'join_room',
                roomId: ROOM_ID,
                name: PLAYER_NAME
            }));
        }

        // Say hello to game server
        ws.send(JSON.stringify({
            type: 'hello',
            name: PLAYER_NAME
        }));
    };

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        handleMessage(data);
    };

    ws.onclose = () => {
        console.log('🔌 Disconnected, reconnecting...');
        setTimeout(connect, 1000);
    };

    ws.onerror = (err) => {
        console.error('WebSocket error:', err);
    };
}

// Handle incoming messages
function handleMessage(data) {
    switch (data.type) {
        case 'welcome':
            myId = data.yourId;
            document.getElementById('player-id').textContent = myId;
            document.getElementById('share-link').value = window.location.href;
            // Connect to LiveKit after getting player ID
            connectLiveKit();
            break;

        case 'room_joined':
            document.getElementById('room-id').textContent = data.roomId;
            showToast(`Joined room ${data.roomId}`);
            if (data.isHost) {
                showToast('You are the host! Share the link to invite friends.');
            }
            break;

        case 'player_joined':
            showToast(`${data.playerName} joined (${data.playerCount} players)`);
            break;

        case 'player_left':
            showToast(`${data.playerName} left`);
            removeVideoTrack(data.playerId);
            break;

        case 'state':
            // Update players from game server
            data.players.forEach(p => {
                if (p.id === myId) {
                    myPlayer.x = p.x;
                    myPlayer.y = p.y;
                    myPlayer.vx = p.vx;
                    myPlayer.vy = p.vy;
                } else {
                    // Smooth interpolation for other players
                    if (!players[p.id]) {
                        players[p.id] = { x: p.x, y: p.y, vx: 0, vy: 0 };
                    }
                    players[p.id].targetX = p.x;
                    players[p.id].targetY = p.y;
                    players[p.id].vx = p.vx;
                    players[p.id].vy = p.vy;
                }
            });
            break;

        case 'error':
            showToast('Error: ' + data.error);
            break;
    }
}

// Input handling
document.addEventListener('keydown', (e) => {
    switch (e.code) {
        case 'KeyW': case 'ArrowUp': keys.up = true; break;
        case 'KeyS': case 'ArrowDown': keys.down = true; break;
        case 'KeyA': case 'ArrowLeft': keys.left = true; break;
        case 'KeyD': case 'ArrowRight': keys.right = true; break;
    }
});

document.addEventListener('keyup', (e) => {
    switch (e.code) {
        case 'KeyW': case 'ArrowUp': keys.up = false; break;
        case 'KeyS': case 'ArrowDown': keys.down = false; break;
        case 'KeyA': case 'ArrowLeft': keys.left = false; break;
        case 'KeyD': case 'ArrowRight': keys.right = false; break;
    }
});

// Mobile controls (if present)
document.querySelectorAll('.dpad-btn').forEach(btn => {
    const dir = btn.dataset.dir;

    btn.addEventListener('touchstart', (e) => {
        e.preventDefault();
        if (dir === 'up') keys.up = true;
        if (dir === 'down') keys.down = true;
        if (dir === 'left') keys.left = true;
        if (dir === 'right') keys.right = true;
    });

    btn.addEventListener('touchend', (e) => {
        e.preventDefault();
        if (dir === 'up') keys.up = false;
        if (dir === 'down') keys.down = false;
        if (dir === 'left') keys.left = false;
        if (dir === 'right') keys.right = false;
    });
});

// Send input to server
function sendInput() {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;

    let dx = 0, dy = 0;
    if (keys.up) dy = -1;
    if (keys.down) dy = 1;
    if (keys.left) dx = -1;
    if (keys.right) dx = 1;

    // Normalize diagonal movement
    if (dx !== 0 && dy !== 0) {
        dx *= 0.707;
        dy *= 0.707;
    }

    ws.send(JSON.stringify({ type: 'input', dx, dy }));
}

// Game loop
let lastTime = 0;
const TICK_RATE = 60;
const INPUT_RATE = 20;

function gameLoop(timestamp) {
    const dt = timestamp - lastTime;
    lastTime = timestamp;

    // Send input at fixed rate
    if (Math.floor(timestamp / (1000 / INPUT_RATE)) !== Math.floor((timestamp - dt) / (1000 / INPUT_RATE))) {
        sendInput();
    }

    // Interpolate other players
    for (const id in players) {
        const p = players[id];
        if (p.targetX !== undefined) {
            p.x += (p.targetX - p.x) * 0.2;
            p.y += (p.targetY - p.y) * 0.2;
        }
    }

    // Render
    render();

    requestAnimationFrame(gameLoop);
}

// Render
function render() {
    ctx.fillStyle = '#12121a';
    ctx.fillRect(0, 0, canvas.width, canvas.height);

    // Center camera on player
    const camX = myPlayer.x - canvas.width / 2;
    const camY = myPlayer.y - canvas.height / 2;

    ctx.save();
    ctx.translate(-camX, -camY);

    // Draw grid
    ctx.strokeStyle = '#1a1a28';
    ctx.lineWidth = 1;
    const gridSize = 50;
    for (let x = 0; x <= 1000; x += gridSize) {
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, 1000);
        ctx.stroke();
    }
    for (let y = 0; y <= 1000; y += gridSize) {
        ctx.beginPath();
        ctx.moveTo(0, y);
        ctx.lineTo(1000, y);
        ctx.stroke();
    }

    // Draw world bounds
    ctx.strokeStyle = '#333';
    ctx.lineWidth = 2;
    ctx.strokeRect(0, 0, 1000, 1000);

    // Draw other players
    for (const id in players) {
        const p = players[id];
        drawPlayer(p.x, p.y, '#00d4ff', false);
    }

    // Draw self
    drawPlayer(myPlayer.x, myPlayer.y, '#00ff88', true);

    ctx.restore();
}

function drawPlayer(x, y, color, isSelf) {
    // Glow
    const gradient = ctx.createRadialGradient(x, y, 0, x, y, 30);
    gradient.addColorStop(0, color + '40');
    gradient.addColorStop(1, 'transparent');
    ctx.fillStyle = gradient;
    ctx.beginPath();
    ctx.arc(x, y, 30, 0, Math.PI * 2);
    ctx.fill();

    // Player dot
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.arc(x, y, isSelf ? 12 : 10, 0, Math.PI * 2);
    ctx.fill();

    // Border
    ctx.strokeStyle = isSelf ? '#fff' : color;
    ctx.lineWidth = 2;
    ctx.stroke();
}

// Toast notifications
function showToast(message) {
    let toast = document.getElementById('toast');
    if (!toast) {
        toast = document.createElement('div');
        toast.id = 'toast';
        document.body.appendChild(toast);
    }
    toast.textContent = message;
    toast.classList.add('show');
    setTimeout(() => toast.classList.remove('show'), 3000);
}

// Copy link to clipboard
function copyLink() {
    const input = document.getElementById('share-link');
    input.select();
    document.execCommand('copy');
    showToast('Link copied!');
}

// Start
connect();
requestAnimationFrame(gameLoop);