# Whitelist Bypass

Tunnels internet traffic through video calling platforms (VK Call, Yandex Telemost, WB Stream, DION, Bitrix24, Blink) to bypass government whitelist censorship.

## Setup

Step-by-step setup guide (in Russian): [docs/SETUP.md](docs/SETUP.md)

## How it works

Two tunnel modes are available: **DC** (DataChannel) and **Video** (VP8 data encoding). The recommended setup is **headless on both ends** - pure Go (Pion) talks to the platform's SFU directly, no browser. ChaCha20 obfuscation, configurable VP8 pacing, and the LiveKit/WB Stream backend are headless-only features.

A legacy browser path (Android `WebView` joiner against an Electron desktop creator with JS hooks) still works for VK DC, but it's slower, can't obfuscate, and is being phased out. New deployments should use headless.

### DC mode

Pion opens a SCTP DataChannel on the publisher PC and tunnels TCP/UDP through it. Frames pass through the platform's SFU like any other DataChannel payload.



```
Joiner (censored, Android/iOS/desktop)              Creator (free internet)

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

Video mode can be wrapped in **KCP** (the joiner's `Reliable` option, `--reliable` on the CLI joiners): lost frames are retransmitted instead of breaking TCP streams on a lossy path. The creator detects it from the joiner's first frame, nothing to configure on that side. Supported on VK, Telemost, WB Stream, Bitrix and Blink.

Traffic goes through the platform's SFU, which is on the government whitelist. To DPI it looks like a normal video call.

## Components

- `relay/` - Go relay shared by both ends: SOCKS5 proxy, tun2socks plumbing, DC/VP8 tunnel, ChaCha20 obfuscator, connection multiplexer, gomobile/iOS bindings
- `headless/vk/` - Headless VK creator: creates or joins a call via the VK HTTP API, Pion DC/VP8 tunnel, no browser
- `headless/telemost/` - Headless Telemost (Yandex) creator with the same model
- `headless/wbstream/` - Headless WB Stream creator (LiveKit-backed, anonymous guest tokens)
- `headless/dion/` - Headless DION creator with the same model
- `headless/bitrix/` - Headless Bitrix creator (adaptive DC/VP8, guest join)
- `headless/blink/` - Headless Blink creator (LiveKit-backed, logs in through the Blink app)
- `headless/wbstream-joiner/` - Desktop WB Stream joiner (counterpart to the creator, used for tests and Linux clients)
- `headless/telemost-joiner/` - Desktop Telemost joiner (counterpart to the creator, used for tests and Linux clients)
- `headless/dion-joiner/` - Desktop DION joiner (counterpart to the creator, used for tests and Linux clients)
- `headless/bitrix-joiner/` - Desktop Bitrix joiner (counterpart to the creator, used for tests and Linux clients)
- `headless/blink-joiner/` - Desktop Blink joiner (counterpart to the creator, used for tests and Linux clients)
- `headless/vk-bot/` - Standalone VK Long Poll bot that spawns headless creators on demand and replies with the join link (server-side alternative to the Electron bot)
- `headless/tests/` - Live end-to-end tests (creator + joiners over a real SFU); run same-host or in a Dockerized netns stand, see [headless/tests/README.md](headless/tests/README.md)
- `android-app/` - Android joiner: VpnService + tun2socks + headless Pion (primary path); also retains a `WebView` fallback for the legacy browser flow
- `ios-proxy-app/` - iOS joiner: SOCKS5 + headless Pion via the gomobile xcframework
- `ios-vpn-app/` - iOS joiner with a system VPN: Network Extension + headless Pion, needs a signing team with the Network Extension capability
- `joiner-desktop-app/` - Electron desktop joiner for Windows / macOS / Linux: system TUN or SOCKS5 only, all six platforms
- `creator-app/` - Electron desktop creator app: GUI front-end that can run either the legacy browser path or spawn the headless Go binaries; suitable for both interactive use and deployments
- `headless/docker/` - Docker image and compose file for the headless VK bot with all creators
- `docker-build/` - Dockerized build of the Android app, the creator app and the headless binaries
- `hooks/` - JavaScript hooks for the legacy browser path (DC and Video modes, VK and Telemost). Headless does not use JS hooks.

## Download

Prebuilt binaries are available on [GitHub Releases](../../releases).

### Creator side (free internet, desktop)

Download and run the Electron app from [GitHub Releases](../../releases). It bundles the headless Go binaries automatically.

1. Open the app and click **+** to add a tab
2. Pick a platform: **VK**, **Telemost**, **WBStream**, **DION**, **Bitrix** or **Blink**
3. Click **Create new**, or **Join existing** to reuse a call link
4. Log in when the app asks - only on the first run of each platform
5. Copy the join link, send it to the joiner

The tunnel mode is chosen on the joiner, the headless creator adapts to it on every connect. To run the creators on a server without a GUI, press **Export Cookies** and use the files with the headless binaries, see [docs/SETUP.md](docs/SETUP.md).

**VK (legacy)** and **Telemost (legacy)** run the old browser path. There the call must be created from within the app and the tunnel mode is picked on the creator - the JS hooks must be present from the moment the call starts.

### Joiner side (censored, Android/iOS/desktop)

Pick whichever fits the device:

- **Android** - install `whitelist-bypass.apk` from [Releases](../../releases). Add the join link to the call list, tap **Connect** and allow the VPN prompt; system-wide traffic flows through the call.
- **iOS, VPN** - `whitelist-bypass-vpn.ipa` from [Releases](../../releases) runs a system VPN like the Android app. It uses a Network Extension, so it must be signed by a team with that capability, which a free Apple account does not have.
- **iOS, proxy** - `whitelist-bypass-proxy.ipa` from [Releases](../../releases) (sideload via AltStore / Sideloadly / a free dev account). Exposes a local SOCKS5 proxy only. To proxy the whole device, point any SOCKS5-capable VPN app (Happ, Shadowrocket, Streisand, ...) at the SOCKS5 endpoint the app shows; or set the proxy per app (Telegram has built-in support).
- **Desktop app** - `WhitelistBypass Joiner` for Windows / macOS / Linux from [Releases](../../releases). Raises a system TUN tunnel for the whole host, or a local SOCKS5 proxy only.
- **CLI** - the `whitelist-bypass-cli-<os>-<arch>.zip` archives from [Releases](../../releases) hold headless joiners for Linux and FreeBSD; each exposes a SOCKS5 proxy on the given port for whatever you point at it. Useful for servers and routers. Optional `--socks-user` / `--socks-pass` enable SOCKS5 username/password auth. There is no CLI joiner for VK, its join needs a captcha solved by hand.
  - WB Stream: `headless-wbstream-joiner --room <link> --socks-port 1080 [--socks-user u --socks-pass p]`
  - Telemost: `headless-telemost-joiner --tm-link <link> --socks-port 1080 [--socks-user u --socks-pass p]`
  - DION: `headless-dion-joiner --room <link> --socks-port 1080 [--socks-user u --socks-pass p]`
  - Bitrix: `headless-bitrix-joiner --link <link> --socks-port 1080 [--socks-user u --socks-pass p]`
  - Blink: `headless-blink-joiner --room <link> --cookies cookies-blink-joiner.json --socks-port 1080 [--socks-user u --socks-pass p]`; run it with `--login --cookies cookies-blink-joiner.json` first

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
# Full release build (Android APK + Creator app + CLI zips + Joiner app + iOS IPAs on macOS)
./make-release.sh

# Individual builds
./build-go.sh          # Go .aar, Android relay, headless creators
./copy-hooks.sh        # Copy JS hooks to android assets
./build-android.sh     # Android APK
./build-headless.sh    # Headless binaries only (current platform)
./build-cli.sh         # Per-OS/arch zips of headless creators + joiners + vk-bot
./build-creator.sh     # Creator Electron app (all platforms)
./build-joiner-app.sh  # Joiner Electron app (all platforms), builds its Go backend too
./build-ios.sh proxy   # iOS proxy app, xcframework + unsigned IPA
./build-ios.sh vpn     # iOS VPN app, xcframework + unsigned IPA
```

### iOS

Requires Xcode and macOS. `build-ios.sh` takes one argument, `proxy` or `vpn`.

```sh
./build-ios.sh proxy   # ios-proxy-app -> prebuilts/whitelist-bypass-proxy.ipa
./build-ios.sh vpn     # ios-vpn-app   -> prebuilts/whitelist-bypass-vpn.ipa
```

The script builds `Mobile.xcframework` into the app directory, runs the app's `strip-signing.sh` to clear the developer team from the project, builds the app unsigned, and writes an unsigned IPA to `prebuilts/`.

To run on a device, open the matching `.xcodeproj` in Xcode, set your team and unique bundle identifiers in Signing & Capabilities, and build. The `vpn` variant requires the Network Extension capability on its bundle identifiers.

Output in `prebuilts/`:

| File | Platform |
|---|---|
| `WhitelistBypass Creator-*-arm64.dmg` / `-x64.dmg` | macOS Apple Silicon / Intel |
| `WhitelistBypass Creator-*-x64.exe` | Windows x64 |
| `WhitelistBypass Creator-*-ia32.exe` | Windows x86 |
| `WhitelistBypass Creator-*-x86_64.AppImage` | Linux x64 |
| `WhitelistBypass Joiner-*-arm64.dmg` / `-x64.dmg` | macOS Apple Silicon / Intel |
| `WhitelistBypass Joiner-*-x64.exe` | Windows x64 |
| `WhitelistBypass Joiner-*-ia32.exe` | Windows x86 |
| `WhitelistBypass Joiner-*-x86_64.AppImage` | Linux x64 |
| `whitelist-bypass.apk` | Android |
| `whitelist-bypass-proxy.ipa` | iOS proxy app, unsigned |
| `whitelist-bypass-vpn.ipa` | iOS VPN app, unsigned |
| `whitelist-bypass-cli-linux-x64.zip` | Linux x64 |
| `whitelist-bypass-cli-linux-ia32.zip` | Linux x86 |
| `whitelist-bypass-cli-linux-arm.zip` | Linux ARM, folders `arm64/` and `arm/` |
| `whitelist-bypass-cli-linux-mips.zip` | Linux MIPS routers, folders `mips/`, `mipsle/`, `mips64/`, `mips64le/` |
| `whitelist-bypass-cli-freebsd-x64.zip` | FreeBSD x64 |
| `whitelist-bypass-cli-freebsd-arm64.zip` | FreeBSD ARM64 |

Each CLI zip holds all six headless creators, five joiners and `headless-vk-bot` under their plain names, e.g. `headless-vk-creator`.

### Docker build

To build the project using Docker, execute:

```sh
docker compose -f docker-build/docker-compose.yml up 
```

This will build the Android app, the creator app and the headless binaries into the `prebuilts` folder (except the macOS creator)

### Headless creators

Pure Go creators that create calls via API without a browser. No Electron, no JS hooks - Go Pion PeerConnection handles the DataChannel tunnel directly.

```sh
./build-headless.sh
```

Twelve binaries are produced - six creators, five Linux joiners, and the VK bot:

```sh
./headless/vk/headless-vk-creator               --cookies cookies-vk.json
./headless/telemost/headless-telemost-creator   --cookies cookies-yandex.json
./headless/wbstream/headless-wbstream-creator   --cookies cookies-wbstream.json
./headless/dion/headless-dion-creator           --cookies cookies-dion.json
./headless/bitrix/headless-bitrix-creator       --cookies cookies-bitrix.json
./headless/blink/headless-blink-creator         --cookies cookies-blink.json
./headless/wbstream-joiner/headless-wbstream-joiner --room <link> --socks-port 1080
./headless/dion-joiner/headless-dion-joiner         --room <link> --socks-port 1080
./headless/telemost-joiner/headless-telemost-joiner --tm-link <link> --socks-port 1080
./headless/bitrix-joiner/headless-bitrix-joiner     --link <link> --socks-port 1080
./headless/blink-joiner/headless-blink-joiner       --room <link> --cookies cookies-blink-joiner.json --socks-port 1080
./headless/vk-bot/headless-vk-bot               --token <t> --group-id <g> --bins-dir <dir>
```

All creators need cookies exported from the desktop creator app (`Export Cookies` button) as JSON `[{"name":"..","value":".."},...]`. DION also accepts `{"email":"..","password":"..","cookies":[]}` and Bitrix `{"portal":"..","email":"..","password":"..","cookies":[]}`, then they log in on their own, see [docs/SETUP.md](docs/SETUP.md).

Blink has no guest access and no password login. Run the creator or the joiner with `--login --cookies <file>`: it prints a link, open it on the phone with the Blink app and confirm. The process rewrites the file on every token refresh, so keep it writable and never share one copy between two processes; if a refresh fails, run `--login` again. The creator and the joiner need separate logins, one account is enough.

#### Common flags

| Flag | VK | TM | WB | DION | BX | BL | Description |
|---|---|---|---|---|---|---|---|
| `--cookies <path>` | yes | yes | yes | yes | yes | yes | path to cookies JSON |
| `--cookie-string <str>` | yes | yes | - | - | - | - | raw cookie string `name=val; name=val` |
| `--write-file <path>` | yes | yes | yes | yes | yes | yes | append the active join link to this file (one link per line) |
| `--resources <mode>` | yes | yes | yes | yes | yes | yes | `default` / `moderate` / `unlimited` / `custom` (see below); DION has no `custom` |
| `--read-buf <bytes>` | yes | yes | yes | - | yes | yes | DC/RTP read buffer; only consulted with `--resources custom` |
| `--max-dc-buf <bytes>` | yes | - | - | - | - | - | DataChannel `BufferedAmountLowThreshold`; only with `--resources custom` |
| `--mem-limit <bytes>` | yes | yes | yes | - | - | yes | Go soft memory limit (`debug.SetMemoryLimit`); only with `--resources custom` |

The full per-platform flag list, including the upstream SOCKS5 and local network options, is in [docs/SETUP.md](docs/SETUP.md).

#### Joining an existing call

By default each binary creates a fresh call. To attach to an existing one (server restarts without invalidating the link, multiple shaped sessions, ...):

| Creator | Flag | Value |
|---|---|---|
| VK | `--vk-link <link>` | `https://vk.ru/call/join/<token>` |
| Telemost | `--tm-link <uri>` | `https://telemost.yandex.ru/j/<id>` |
| WB Stream | `--room <id>` | `wbstream://<uuid>`, `https://stream.wb.ru/room/<uuid>` or just the UUID |
| DION | `--room <id>` | `dion://<id>` or `https://dion.vc/event/<id>` |
| Bitrix | `--room <link>` | `https://<portal>.bitrix24.ru/video/<alias>` or just the alias |
| Blink | `--room <id>` | `blink://<chat id>`, the chat id, or a group invite link |

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
./headless-vk-creator --cookies cookies-vk.json --vk-link https://vk.ru/call/join/<token> \
  --resources custom --read-buf 65536 --max-dc-buf 8388608 --mem-limit 268435456 \
  --write-file /var/run/whitelist-bypass/call.txt
```

- `read-buf` - TCP/DC read buffer size. Smaller = more frequent backpressure checks, less bursty memory
- `max-dc-buf` - pauses TCP reads when the DataChannel buffered amount exceeds this; only wired in VK (Pion `BufferedAmountLowThreshold`)
- `mem-limit` - Go runtime soft memory limit; makes GC more aggressive near the cap

## License

[MIT](LICENSE)

## Donate

- `0xd986b7576340d8d7b04f806dfd38a182b19edf50` - USDC (ERC20)
- `TTEo4XXTB6CqhEiKpyoncfk3skEvoq3bCP` - USDT (TRC20)
- `UQCZge2JzLmwNtI56p-ZxLuN2f7ZMCPGqO0XTt3wJ-cy8JaE` - USDT (TON)
