# headless e2e tests

End-to-end tests for the headless creators and joiners.

Each run starts one creator per platform and keeps it running. Joiners then
connect to that creator one at a time, using different parameters. This tests how
the creator adapts when joiner parameters change.

Each test sends a unique string through the joiner's SOCKS5 proxy, over the
tunnel, to a listener on the creator side. The test passes if the string arrives.
No external network is used.

## Platforms and scenarios

Platforms: telemost, wbstream, dion, bitrix, blink, vk. A platform skips the scenarios it
does not support: dion supports only connect and kick, vk only connect, dc and kcp, blink
everything except kick.

`vk` runs only when named, needs `relay/relay` from `./build-go.sh`, and opens the VK
captcha in your browser.

| Scenario | Joiner |
|----------|--------|
| connect  | Default video tunnel. |
| dc       | `--tunnel-mode dc`. |
| kcp      | `--reliable` (KCP). |
| dual     | `--dual-track`. |
| kick     | Two joiners. The first is expected to be kicked. |

## Rooms

Copy `.env.example` to `.env`. Set `ROOM_<PLATFORM>` to an existing conference link
for each platform you want to test.

If a room is empty, the harness skips that platform. It does not create a new
conference. To create new conferences, set `ALLOW_NEW_ROOMS=1`.

## Cookies

Each platform reads `cookies-<platform>.json` from the repository root. Export
these from the desktop creator app. If a cookie file is missing, the harness skips
that platform.

## Blink

Blink has no guest access, so the creator and the joiner each need their own login.
Both can be the same account, but every login must be approved separately. Create the
two files from the repository root. Each command prints a login link, open it on the
phone with the Blink app:

    ./headless/blink/headless-blink-creator --login --cookies cookies-blink.json
    ./headless/blink/headless-blink-creator --login --cookies cookies-blink-joiner.json

`ROOM_BLINK` is a chat id. The creator joins the call in that chat, and starts one if
the chat has no active call. The joiner's account must be a member of the chat.

## Run in Docker

The creator and joiner run in separate network namespaces so their WebRTC ICE does
not collide. This requires Docker.

    cd headless/tests/stand
    ./stand.sh build
    ./stand.sh run
    ./stand.sh run bitrix dc kick
    ./stand.sh sh

`build` cross-compiles the Linux binaries and builds the image. `run` runs the
tests; with no arguments it runs every supported platform and scenario. `run`
mounts the cookie files from the repository root, so refreshed tokens are written
back to them. `sh` opens a shell in the container.

## Run on the host

    ./build-headless.sh
    ./headless/tests/run.sh
    ./headless/tests/run.sh bitrix

Run `build-headless.sh` from the repository root first. Same-host WebRTC video can
fail because the creator and joiner share an IP address. Use the Docker method for
the video scenarios.

## Environment variables

| Variable | Description |
|----------|-------------|
| `FAIL_FAST` | Stop at the first failure. Default `1`. Set to `0` to run all tests. |
| `CREATE_TIMEOUT` | Seconds to wait for the creator to start. |
| `CONNECT_TIMEOUT` | Seconds to wait for a joiner's SOCKS port. |
| `PROBE_TIMEOUT` | Seconds to wait for tunnel traffic. |
| `KICK_TIMEOUT` | Seconds to wait for a kick to take effect. |
| `PORT_BASE` | First local port to allocate. |
| `COOKIES_DIR` | Directory that holds the cookie files. |
