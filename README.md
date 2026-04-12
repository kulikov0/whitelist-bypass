# Whitelist Bypass

Tunnels internet traffic through video calling platforms (VK Call, Yandex Telemost) to bypass government whitelist censorship.

## How it works

Two tunnel modes are available: **DC** (DataChannel) and **Pion Video** (VP8 data encoding).

### DC mode

Browser-based. JavaScript hooks intercept RTCPeerConnection on the call page, create a DataChannel alongside the call's built-in channels, and use it as a bidirectional data pipe.

- **VK Call** - Negotiated DataChannel id:2 (alongside VK's animoji channel id:1). Data flows through VK's SFU
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
DataChannel  <----- SFU ----->   DataChannel
                                            |
                                        WebSocket (:9000)
                                            |
                                        Go relay
                                            |
                                        Internet
```

### Pion Video mode

Go-based. Pion (Go WebRTC library) connects directly to the platform's TURN/SFU servers, bypassing the browser's WebRTC stack entirely. Data is encoded inside VP8 video frames.

- **VK Call** - Single PeerConnection, data flows through VK's SFU
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
Pion WebRTC  <------ SFU ------>  Pion WebRTC
                                            |
                                        Relay bridge
                                            |
                                        Internet
```

Traffic goes through the platform's SFU servers which are whitelisted. To the network firewall it looks like a normal video call.

## Components

- `hooks/` - JavaScript hooks for DC, Video, and Headless modes (VK and Telemost)
- `relay/` - Go relay: SOCKS5 proxy, WebSocket server, VP8 video tunnel, headless joiner, connection multiplexing
- `headless/vk/` - Headless VK creator: creates calls via API, Pion DataChannel tunnel, no browser
- `headless/telemost/` - Headless Telemost creator: same approach for Yandex Telemost
- `android-app/` - Android joiner app (WebView/headless + VpnService + Go relay)
- `creator-app/` - Electron desktop creator app

## Download

Prebuilt binaries are available on [GitHub Releases](../../releases).

## Setup

Step-by-step setup guide (in Russian): [telegra.ph](https://telegra.ph/Rabota-s-whitelist-bypass-03-29)

### Creator side (free internet, desktop)

Download and run the Electron app from [GitHub Releases](../../releases). It bundles the Go relay automatically.

1. Open the app
2. Select tunnel mode (DC or Pion Video)
3. Click "VK" or "Telemost"
4. Log in, **create a new call** from the app
5. Copy the join link, send it to the joiner

**Important:** The call must be created from within the Creator app. Joining an existing call from the app will not work - the JS hooks must be present from the moment the call starts.

### Joiner side (censored, Android)

1. Download and install `whitelist-bypass.apk` from [GitHub Releases](../../releases)
2. Select tunnel mode (DC or Pion Video)
3. Paste the call link and tap GO
4. The app joins the call, establishes the tunnel, starts VPN
5. All device traffic flows through the call

## Building from source

### Requirements

- Go 1.26+
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

### Docker build

To build the project using Docker, execute:

```sh
docker compose -f docker-build/docker-compose.yml up 
```

This will build all components (creator-app, headless, android app) into the `prebuild` folder (except the macOS creator)

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

### Headless creators

Pure Go creators that create calls via API without a browser. No Electron, no JS hooks - Go Pion PeerConnection handles the DataChannel tunnel directly.

```sh
# VK
cd headless/vk && go build -o headless-vk-creator .
./headless-vk-creator --cookies cookies.json [--peer-id <vk_peer_id>] [--resources <mode>] [--write-file call-vk]

# Telemost
cd headless/telemost && go build -o headless-telemost .
./headless-telemost --cookies cookies-yandex.json [--resources <mode>] [--write-file call-telemost]
```

- `--cookies` - path to cookies exported as JSON (`[{"name":"..","value":".."},...]`)
- `--peer-id` - VK peer_id for the call (VK only, optional)
- `--resources` - resource mode (see below)
- `--write-file` - path to file where the active call link is appended (one link per line, created if missing)

**Resource modes:**

| Mode | read-buf | max-dc-buf | mem-limit | Use case |
|---|---|---|---|---|
| `moderate` | 16KB | 1MB | 64MB | Low memory environments, VPS |
| `default` | 32KB | 4MB | 128MB | General use |
| `unlimited` | 64KB | 8MB | 256MB | Maximum throughput |

- `read-buf` - TCP read buffer size. Smaller = more frequent backpressure checks, less bursty memory
- `max-dc-buf` - pauses TCP reads when DataChannel buffered amount exceeds this. Prevents SCTP pending queue from growing unbounded
- `mem-limit` - Go runtime soft memory limit (`debug.SetMemoryLimit`), makes GC more aggressive near the cap

## License

[MIT](LICENSE)
