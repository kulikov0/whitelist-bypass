# Whitelist Bypass

Tunnels internet traffic through video calling platforms (VK Call, Yandex Telemost) to bypass government whitelist censorship.

## How it works

Two tunnel modes are available: **DC** (DataChannel) and **Pion Video** (VP8 data encoding).

### DC mode

Browser-based. JavaScript hooks intercept RTCPeerConnection on the call page, create a DataChannel alongside the call's built-in channels, and use it as a bidirectional data pipe.

- **VK Call** - Negotiated DataChannel id:2 (alongside VK's animoji channel id:1). P2P via TURN relay
- **Telemost** - Non-negotiated DataChannel labeled "sharing" (matching real screen sharing traffic), with SDP renegotiation via signaling WebSocket. SFU architecture

```
Joiner (censored, Android)                Creator (free internet, desktop)

All apps
  |
VpnService (captures all traffic)
  |
tun2socks (IP -> TCP)
  |
SOCKS5 proxy (Go, :1080)
  |
WebSocket (:9000)
  |
WebView (call page)                       Electron (call page)
  |                                         |
DataChannel  <--- TURN/SFU --->   DataChannel
                                            |
                                        WebSocket (:9000)
                                            |
                                        Go relay
                                            |
                                        Internet
```

### Pion Video mode

Go-based. Pion (Go WebRTC library) connects directly to the platform's TURN/SFU servers, bypassing the browser's WebRTC stack entirely. Data is encoded inside VP8 video frames.

- **VK Call** - Single PeerConnection, P2P via TURN relay
- **Telemost** - Dual PeerConnection (pub/sub), SFU architecture

The JS hook replaces `RTCPeerConnection` with a `MockPeerConnection` that forwards all SDP/ICE operations to the local Pion server via WebSocket. Pion creates the real PeerConnection with the platform's TURN servers.

**VP8 data encoding:**
- Data frames: `[0xFF marker][4B length][payload]` - sent as VP8 video samples
- Keepalive frames: valid VP8 interframes (17 bytes) at 25fps, keyframe every 60th frame. Keeps the video track alive so the SFU/TURN does not disconnect
- The `0xFF` marker byte distinguishes data from real VP8 (keyframe first byte has bit0=0, interframe has bit0=1, so `0xFF` never appears naturally)
- On the receiving side, RTP packets are reassembled into full frames. First byte `0xFF` = extract data, otherwise = keepalive, ignore

**Multiplexing protocol** over the VP8 tunnel: `[4B frame length][4B connID][1B msgType][payload]`
- Message types: Connect, ConnectOK, ConnectErr, Data, Close, UDP, UDPReply
- Multiple TCP/UDP connections are multiplexed into a single VP8 video stream

```
Joiner (censored, Android)                Creator (free internet, desktop)

All apps
  |
VpnService (captures all traffic)
  |
tun2socks (IP -> TCP)
  |
SOCKS5 proxy (Go, :1080)
  |
VP8 data tunnel (Pion)                    VP8 data tunnel (Pion)
  |                                         |
MockPC (WebView)                          MockPC (Electron)
  |                                         |
Pion WebRTC  <--- TURN/SFU --->   Pion WebRTC
                                            |
                                        Relay bridge
                                            |
                                        Internet
```

Traffic goes through the platform's TURN servers which are whitelisted. To the network firewall it looks like a normal video call.

## Components

- `hooks/` - JavaScript hooks injected into call pages
  - `joiner-vk.js`, `creator-vk.js` - VK Call DC hooks
  - `joiner-telemost.js`, `creator-telemost.js` - Telemost DC hooks
  - `pion-vk.js`, `pion-telemost.js` - Pion Video hooks (MockPeerConnection mode)
  - DC hooks intercept RTCPeerConnection, create tunnel DataChannel, bridge to local WebSocket
  - Pion hooks replace RTCPeerConnection with MockPC, forward SDP/ICE to Pion via WebSocket
  - Telemost hooks include fake media (camera/mic), message chunking (994B payload, 1000B total), and SDP renegotiation
- `relay/` - Go relay binary and gomobile library
  - `relay/mobile/` - DC mode: SOCKS5 proxy, WebSocket server, binary framing protocol
  - `relay/pion/` - Pion Video mode: VP8 data tunnel, relay bridge, SOCKS5 proxy
    - `common.go` - Shared types, WebSocket helper, ICE server parsing, AndroidNet
    - `vk.go` - VK Pion client (single PeerConnection, P2P)
    - `telemost.go` - Telemost Pion client (dual PeerConnection, pub/sub)
    - `vp8tunnel.go` - VP8 frame encoding/decoding, keepalive generation
    - `relay.go` - Relay bridge with connection multiplexing, SOCKS5 proxy, UDP ASSOCIATE
  - `relay/mobile/tun_android.go` - Android-only: tun2socks + fdsan fix (CGo)
  - `relay/mobile/tun_stub.go` - Desktop stub (no tun2socks needed)
- `android-app/` - Android joiner app
  - WebView loading call page with hook injection (hidden from user)
  - VpnService capturing all device traffic
  - Tunnel mode selector (DC / Pion Video, long-press Connect button)
  - Auto-connect: fetches current call link from link server, joins automatically
  - Go relay as .aar library (gomobile) + Pion relay as native binary
- `creator-app/` - Electron desktop creator app
  - Webview with persistent session for login retention
  - CSP header stripping for localhost WebSocket access
  - Auto-permission granting (camera/mic)
  - Tunnel mode selector (DC / Pion Video)
  - Go relay spawned as child process
  - Log panels for relay and hook output

## Download

Prebuilt binaries are available on [GitHub Releases](../../releases).

## Setup

### Option A: Manual (no server required)

The simplest setup — creator manually shares the call link each session.

**Creator side (free internet, desktop)**

Download and run the Electron app from [GitHub Releases](../../releases). It bundles the Go relay automatically.

1. Open the app
2. Select tunnel mode (DC or Pion Video)
3. Click "VK Call" or "Telemost"
4. Log in, create a call
5. Copy the join link, send it to the joiner

**Joiner side (censored, Android)**

1. Download and install `whitelist-bypass.apk` from [GitHub Releases](../../releases)
2. Select tunnel mode (DC or Pion Video)
3. Paste the call link and tap Connect
4. The app joins the call, establishes the tunnel, starts VPN
5. All device traffic flows through the call

---

### Option B: Always-on server (auto-connect)

This setup runs the creator headlessly on a VPS with free internet. The Android app fetches the current call link automatically — no manual link sharing needed. The WebView is hidden from the user; the app shows only a Connect button.

**Architecture**

```
VPS (free internet)
  ├── creator-app (Electron + Xvfb) — keeps a VK call open 24/7
  ├── link-server (Node.js :8080)   — exposes current call link via HTTP
  └── relay (Go :9000)              — bridges DataChannel ↔ internet

Android (censored)
  └── App fetches http://VPS_IP:8080/link → joins call silently → VPN starts
```

**1. VPS requirements**

Any Linux VPS with public IP on free internet, Node.js 18+, and a virtual display (Xvfb) for headless Electron.

**2. Collect VK session cookies**

The creator app needs an active VK session. Easiest method:

1. Log into [vk.com](https://vk.com) in Chrome on any machine
2. Open DevTools → Application → Storage → Cookies → `https://vk.com`
3. Copy key cookies: `remixsid`, `remixsid_https`, `remixlang`, `remixlhk`

Or run the creator Electron app on a desktop, log in via GUI, then copy the session folder to the server:
- macOS: `~/Library/Application Support/whitelist-bypass-creator/`
- Linux: `~/.config/whitelist-bypass-creator/`
- Windows: `%APPDATA%\whitelist-bypass-creator\`

**3. Deploy link server on VPS**

Save as `/opt/whitelist-bypass/link-server.js`:

```js
const http = require('http');
let currentLink = '';

http.createServer((req, res) => {
  res.setHeader('Access-Control-Allow-Origin', '*');
  if (req.method === 'POST' && req.url === '/link') {
    let body = '';
    req.on('data', d => body += d);
    req.on('end', () => { currentLink = JSON.parse(body).link; res.end('ok'); });
  } else if (req.url === '/link') {
    res.end(JSON.stringify({ link: currentLink }));
  } else {
    res.writeHead(404); res.end();
  }
}).listen(8080, () => console.log('Link server on :8080'));
```

**4. Run creator headlessly**

```sh
# Install Xvfb
apt-get install -y xvfb

# Start virtual display
Xvfb :99 -screen 0 1280x800x24 &

# Run creator app
DISPLAY=:99 ./WhitelistBypass-linux.AppImage --no-sandbox &

# Run link server
node /opt/whitelist-bypass/link-server.js &
```

**5. Auto-post call link from creator app**

In the creator app, after the VK call page loads, add a script to post the join link to the link server. You can paste this in the creator app's JS console:

```js
// Run once after call is created
fetch('http://localhost:8080/link', {
  method: 'POST',
  body: JSON.stringify({ link: window.location.href })
});
```

Or automate it by adding it to the hook file (`hooks/creator-vk.js`) — the hook already runs on page load.

**6. Build Android APK pointing to your VPS**

```sh
./build-app.sh -PlinkServerUrl=http://YOUR_VPS_IP:8080/link
```

**7. Run as systemd services**

```ini
# /etc/systemd/system/vk-xvfb.service
[Unit]
Description=Xvfb virtual display
[Service]
ExecStart=/usr/bin/Xvfb :99 -screen 0 1280x800x24
Restart=always
[Install]
WantedBy=multi-user.target
```

```ini
# /etc/systemd/system/vk-creator.service
[Unit]
Description=VK call creator (headless)
After=vk-xvfb.service network-online.target
[Service]
Environment=DISPLAY=:99
ExecStart=/opt/whitelist-bypass/WhitelistBypass-linux.AppImage --no-sandbox
Restart=always
[Install]
WantedBy=multi-user.target
```

```ini
# /etc/systemd/system/vk-linkserver.service
[Unit]
Description=VK call link server
[Service]
ExecStart=/usr/bin/node /opt/whitelist-bypass/link-server.js
Restart=always
[Install]
WantedBy=multi-user.target
```

```sh
systemctl enable --now vk-xvfb vk-creator vk-linkserver
```

## Building from source

### Requirements

- Go 1.21+
- gomobile (`go install golang.org/x/mobile/cmd/gomobile@latest`)
- gobind (`go install golang.org/x/mobile/cmd/gobind@latest`)
- Android SDK + NDK 29
- Java 17+
- Node.js 18+

### Build scripts

```sh
# Build Go .aar and Pion relay for Android (includes hooks copy)
./build-go.sh

# Build Android APK -> prebuilts/whitelist-bypass.apk
./build-app.sh

# Build APK with custom link server URL (Option B)
./build-app.sh -PlinkServerUrl=http://YOUR_VPS_IP:8080/link

# Build Electron apps for all platforms -> prebuilts/
./build-creator.sh
```

Output in `prebuilts/`:

| File | Platform |
|---|---|
| `WhitelistBypass Creator-*-arm64.dmg` | macOS |
| `WhitelistBypass Creator-*-x64.exe` | Windows x64 |
| `WhitelistBypass Creator-*-ia32.exe` | Windows x86 |
| `WhitelistBypass Creator-*.AppImage` | Linux x64 |
| `whitelist-bypass.apk` | Android |

### Relay

```
relay --mode <mode> [--ws-port 9000] [--socks-port 1080]
```

- `--mode` - required: `joiner`, `creator`, `vk-video-joiner`, `vk-video-creator`, `telemost-video-joiner`, `telemost-video-creator`
- `--ws-port` - WebSocket port for browser/hook connection (default 9000)
- `--socks-port` - SOCKS5 proxy port, joiner modes only (default 1080)

The Go relay is split into platform-specific files:
- `relay/mobile/mobile.go` - Shared networking code (SOCKS5, WebSocket, framing)
- `relay/mobile/tun_android.go` - Android-only: tun2socks + fdsan fix (CGo)
- `relay/mobile/tun_stub.go` - Desktop stub (no tun2socks needed)

This allows cross-compiling the relay for macOS/Windows/Linux without CGo or Android NDK.

## License

[MIT](LICENSE)
