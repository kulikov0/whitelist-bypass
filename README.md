# Whitelist Bypass

Tunnels internet traffic through video calling platforms (VK Call, Yandex Telemost, WB Stream) to bypass government whitelist censorship.

## Setup

Step-by-step setup guide (in Russian): [docs/SETUP.md](docs/SETUP.md)

## How it works

Two tunnel modes are available: **DC** (DataChannel) and **Video** (VP8 data encoding). The recommended setup is **headless on both ends** - pure Go (Pion) talks to the platform's SFU directly, no browser. ChaCha20 obfuscation, configurable VP8 pacing, and the LiveKit/WB Stream backend are headless-only features.

A legacy browser path (Android `WebView` joiner against an Electron desktop creator with JS hooks) still works for VK DC, but it's slower, can't obfuscate, and is being phased out. New deployments should use headless.

### DC mode

Pion opens a SCTP DataChannel on the publisher PC and tunnels TCP/UDP through it. Frames pass through the platform's SFU like any other DataChannel payload.



```
Joiner (censored, Android/iOS/Linux)                Creator (free internet)

All apps
  |
VpnService (captures all traffic)
  |
tun2socks (IP -> TCP)
  |
SOCKS5 proxy (Go, :1080)
  |
Headless joiner (Pion)                    Headless creator (Pion)
  |                                         |
DataChannel  <------- SFU ------->   DataChannel
                                            |
                                        Relay bridge
                                            |
                                        Internet
```

### Video mode

Same pipeline as DC mode, but the tunnel rides on a published VP8 video track instead of a SCTP DataChannel. Useful when the SFU rate-limits DataChannels but lets RTP through (e.g. Telemost), and platform-mandatory in some cases (WB Stream's publisher track is always video). Tunnel framing and the multiplex layer above it are the same as DC mode.

Traffic goes through the platform's SFU, which is on the government whitelist. To DPI it looks like a normal video call.

## Components

- `relay/` - Go relay shared by both ends: SOCKS5 proxy, tun2socks plumbing, DC/VP8 tunnel, ChaCha20 obfuscator, connection multiplexer, gomobile/iOS bindings
- `headless/vk/` - Headless VK creator: creates or joins a call via the VK HTTP API, Pion DC/VP8 tunnel, no browser
- `headless/telemost/` - Headless Telemost (Yandex) creator with the same model
- `headless/wbstream/` - Headless WB Stream creator (LiveKit-backed, anonymous guest tokens)
- `headless/wbstream-joiner/` - Desktop WB Stream joiner (counterpart to the creator, used for tests and Linux clients)
- `headless/telemost-joiner/` - Desktop Telemost joiner (counterpart to the creator, used for tests and Linux clients)
- `headless/vk-bot/` - Standalone VK Long Poll bot that spawns headless creators on demand and replies with the join link (server-side alternative to the Electron bot)
- `headless/tests/` - End-to-end smoke tests for each platform
- `android-app/` - Android joiner: VpnService + tun2socks + headless Pion (primary path); also retains a `WebView` fallback for the legacy browser flow
- `ios-proxy-app/` - iOS joiner: SOCKS5 + headless Pion via the gomobile xcframework
- `creator-app/` - Electron desktop creator app: GUI front-end that can run either the legacy browser path or spawn the headless Go binaries; suitable for both interactive use and deployments
- `hooks/` - JavaScript hooks for the legacy browser path (DC and Video modes, VK and Telemost). Headless does not use JS hooks.

## Download

Prebuilt binaries are available on [GitHub Releases](../../releases).

### Creator side (free internet, desktop)

Download and run the Electron app from [GitHub Releases](../../releases). It bundles the Go relay automatically.

1. Open the app
2. Select tunnel mode (DC or Video)
3. Click "VK" or "Telemost"
4. Log in, **create a new call** from the app
5. Copy the join link, send it to the joiner

**Important:** The call must be created from within the Creator app. Joining an existing call from the app will not work - the JS hooks must be present from the moment the call starts.

### Joiner side (censored, Android/iOS/Linux)

Three forms are available; pick whichever fits the device:

- **Android** - install `whitelist-bypass.apk` from [Releases](../../releases). Allow the VPN prompt on first launch. Paste the join link and tap GO; system-wide traffic flows through the call.
- **iOS** - install `whitelist-bypass-proxy.ipa` from [Releases](../../releases) (sideload via AltStore / Sideloadly / your dev account). Exposes a local SOCKS5 proxy only - no system VPN. To proxy the whole device, point any SOCKS5-capable VPN app (Shadowrocket, Streisand, ...) at the SOCKS5 endpoint the app shows; or set the proxy per app (Telegram has built-in support).
- **Linux desktop** - run a headless joiner; it exposes a SOCKS5 proxy on the given port for whatever you point at it. Useful for servers and Linux clients. Optional `--socks-user` / `--socks-pass` enable SOCKS5 username/password auth.
  - WB Stream: `headless-wbstream-joiner --room <link> --socks-port 1080 [--socks-user u --socks-pass p]`
  - Telemost: `headless-telemost-joiner --tm-link <link> --socks-port 1080 [--socks-user u --socks-pass p]`

The full step-by-step (Russian) covers each platform in detail: see [docs/SETUP.md](docs/SETUP.md).

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
# Full release build (Android APK + Creator app + Headless creators + Linux joiners + VK bot + iOS IPA on macOS)
./make-release.sh

# Individual builds
./build-go.sh          # Go .aar, relay binary, headless creators
./copy-hooks.sh        # Copy JS hooks to android assets
./build-app.sh         # Android APK
./build-headless.sh    # Headless binaries only (current platform)
./build-cli.sh         # Per-arch zips of headless creators + joiners + vk-bot
./build-creator.sh     # Creator Electron app (all platforms)
./build-ios.sh         # Go .xcframework for iOS
```

### iOS

Requires Xcode and macOS.

```sh
./build-ios.sh
```

This builds `Mobile.xcframework` into `ios-proxy-app/`. Then open `ios-proxy-app/whitelist-bypass-proxy.xcodeproj` in Xcode, select your signing team in Signing & Capabilities, and build to device.

Before committing, run `ios-proxy-app/strip-signing.sh` to remove your Apple developer team ID from the project.

Output in `prebuilts/`:

| File | Platform |
|---|---|
| `WhitelistBypass Creator-*-arm64.dmg` | macOS |
| `WhitelistBypass Creator-*-x64.exe` | Windows x64 |
| `WhitelistBypass Creator-*-ia32.exe` | Windows x86 |
| `WhitelistBypass Creator-*.AppImage` | Linux x64 |
| `whitelist-bypass.apk` | Android |
| `whitelist-bypass-proxy.ipa` | iOS, unsigned |
| `headless-vk-creator-linux-x64` | Linux x64 |
| `headless-vk-creator-linux-ia32` | Linux x86 |
| `headless-telemost-creator-linux-x64` | Linux x64 |
| `headless-telemost-creator-linux-ia32` | Linux x86 |
| `headless-wbstream-joiner-linux-x64` | Linux x64 |
| `headless-wbstream-joiner-linux-ia32` | Linux x86 |
| `headless-telemost-joiner-linux-x64` | Linux x64 |
| `headless-telemost-joiner-linux-ia32` | Linux x86 |
| `headless-vk-bot-linux-x64` | Linux x64 |
| `headless-vk-bot-linux-ia32` | Linux x86 |

### Docker build

To build the project using Docker, execute:

```sh
docker compose -f docker-build/docker-compose.yml up 
```

This will build all components (creator-app, headless, android app) into the `prebuild` folder (except the macOS creator)

### Headless creators

Pure Go creators that create calls via API without a browser. No Electron, no JS hooks - Go Pion PeerConnection handles the DataChannel tunnel directly.

```sh
./build-headless.sh
```

Eight binaries are produced - four creators, three Linux joiners, and the VK bot:

```sh
./headless/vk/headless-vk-creator               --cookies cookies-vk.json
./headless/telemost/headless-telemost-creator   --cookies cookies-yandex.json
./headless/wbstream/headless-wbstream-creator   --cookies cookies-wbstream.json
./headless/dion/headless-dion-creator           --cookies cookies-dion.json
./headless/wbstream-joiner/headless-wbstream-joiner --room <link> --socks-port 1080
./headless/dion-joiner/headless-dion-joiner         --room <link> --socks-port 1080
./headless/telemost-joiner/headless-telemost-joiner --tm-link <link> --socks-port 1080
./headless/vk-bot/headless-vk-bot               --token <t> --group-id <g> --bins-dir <dir>
```

All four creators need cookies exported from the desktop creator app (`Export Cookies` button) as JSON `[{"name":"..","value":".."},...]`. DION also accepts `{"email":"..","password":"..","cookies":[]}` and then logs in on its own, see [docs/SETUP.md](docs/SETUP.md).

#### Common flags

| Flag | VK | TM | WB | Description |
|---|---|---|---|---|
| `--cookies <path>` | yes | yes | yes | path to cookies JSON |
| `--cookie-string <str>` | yes | yes | - | raw cookie string `name=val; name=val` |
| `--write-file <path>` | yes | yes | yes | append the active join link to this file (one link per line) |
| `--resources <mode>` | yes | yes | yes | `default` / `moderate` / `unlimited` / `custom` (see below) |
| `--read-buf <bytes>` | yes | yes | yes | DC/RTP read buffer; only consulted with `--resources custom` |
| `--max-dc-buf <bytes>` | yes | - | - | DataChannel `BufferedAmountLowThreshold`; only with `--resources custom` |
| `--mem-limit <bytes>` | yes | yes | yes | Go soft memory limit (`debug.SetMemoryLimit`); only with `--resources custom` |

#### Joining an existing call

By default each binary creates a fresh call. To attach to an existing one (server restarts without invalidating the link, multiple shaped sessions, ...):

| Creator | Flag | Value |
|---|---|---|
| VK | `--vk-link <link>` | `https://vk.com/call/join/<token>` |
| Telemost | `--tm-link <uri>` | `https://telemost.yandex.ru/j/<id>` |
| WB Stream | `--room <id>` | `wbstream://<uuid>` or just the UUID |
| DION | `--room <id>` | `dion://<id>` or `https://dion.vc/event/<id>` |

Mutually exclusive with the call-creation flags (`--peer-id` for VK; the others have no creation flag).

#### Resource modes

| Mode | `read-buf` | `max-dc-buf` (VK) | `mem-limit` |
|---|---|---|---|
| `moderate` | 16 KB | 1 MB | 64 MB |
| `default`  | 32 KB | 4 MB | 128 MB |
| `unlimited`| 64 KB | 8 MB | 256 MB |
| `custom`   | from `--read-buf` | from `--max-dc-buf` | from `--mem-limit` |

`custom` falls back to `unlimited` defaults for any flag left unset, so partial overrides work:

```sh
./headless-vk-creator --cookies cookies-vk.json --vk-link https://vk.com/call/join/<token> \
  --resources custom --read-buf 65536 --max-dc-buf 8388608 --mem-limit 268435456 \
  --write-file /var/run/whitelist-bypass/call.txt
```

- `read-buf` - TCP/DC read buffer size. Smaller = more frequent backpressure checks, less bursty memory
- `max-dc-buf` - pauses TCP reads when the DataChannel buffered amount exceeds this; only wired in VK (Pion `BufferedAmountLowThreshold`)
- `mem-limit` - Go runtime soft memory limit; makes GC more aggressive near the cap

#### Running unattended

`Restart=always` keeps the process alive, which is not the same as keeping the
tunnel working. A creator can sit there joined and accepting connections while
no payload comes back through any of them - "connected, no internet" on the
phone, a perfectly healthy process to systemd.

[`headless/ops/`](headless/ops/) has the pieces for running it without
supervision: templated systemd units, hourly log rotation, and a watchdog that
tells a deaf tunnel apart from an idle one by comparing `first read` against
`EOF with no data read` in the relay log. It ships with a replay tool, because
the thresholds were calibrated on one tunnel and you should check them against
your own logs before trusting them.

## License

[MIT](LICENSE)
