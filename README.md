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
  - WebView loading call page with hook injection
  - VpnService capturing all device traffic
  - Tunnel mode selector (DC / Pion Video)
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

Step-by-step setup guide (in Russian): [telegra.ph](https://telegra.ph/Rabota-s-whitelist-bypass-03-29)

### Creator side (free internet, desktop)

Download and run the Electron app from [GitHub Releases](../../releases). It bundles the Go relay automatically.

1. Open the app
2. Select tunnel mode (DC or Pion Video)
3. Click "VK Call" or "Telemost"
4. Log in, create a call
5. Copy the join link, send it to the joiner

### Joiner side (censored, Android)

1. Download and install `whitelist-bypass.apk` from [GitHub Releases](../../releases)
2. Select tunnel mode (DC or Pion Video)
3. Paste the call link and tap GO
4. The app joins the call, establishes the tunnel, starts VPN
5. All device traffic flows through the call

## Building from source

### Requirements

- Go 1.21+
- gomobile (`go install golang.org/x/mobile/cmd/gomobile@latest`)
- gobind (`go install golang.org/x/mobile/cmd/gobind@latest`)
- Android SDK + NDK 29
- Java 11+
- Node.js 18+

### Build scripts

```sh
# Build Go .aar and Pion relay for Android (includes hooks copy)
./build-go.sh

# Build Android APK -> prebuilts/whitelist-bypass.apk
./build-app.sh

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
