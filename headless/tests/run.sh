#!/bin/sh
set -u

DIR="$(cd "$(dirname "$0")" && pwd)"
E2E_ROOT="$(cd "$DIR/../.." && pwd)"
HEADLESS="$E2E_ROOT/headless"

[ -f "$DIR/.env" ] && . "$DIR/.env"

. "$DIR/lib.sh"
. "$DIR/platforms.sh"
. "$DIR/scenarios.sh"

CREATE_TIMEOUT="${CREATE_TIMEOUT:-45}"
CONNECT_TIMEOUT="${CONNECT_TIMEOUT:-45}"
PROBE_TIMEOUT="${PROBE_TIMEOUT:-20}"
KICK_TIMEOUT="${KICK_TIMEOUT:-30}"

ALL_PLATFORMS="telemost wbstream dion bitrix"
ALL_SCENARIOS="connect dc kcp dual kick"

usage() {
    cat >&2 <<EOF
Usage: $0 [platform...] [scenario...]

Live end-to-end harness: brings up a headless creator, connects one or more
headless joiners, and proves each tunnel by echoing a nonce through the joiner's
SOCKS5 to a local sink (no external network, no rate limits).

Platforms: $ALL_PLATFORMS
Scenarios: $ALL_SCENARIOS
  connect  one joiner, default video tunnel
  dc       one joiner, --tunnel-mode dc
  kcp      one joiner, --reliable (KCP)
  dual     one joiner, --dual-track
  kick     two joiners on one room, expect the first to be kicked

No args runs every applicable scenario on every platform. Unsupported
platform/scenario pairs are skipped.

Cookies: COOKIES_DIR (default repo root) holds cookies-<platform>.json, or set
COOKIES_<PLATFORM> to a explicit path. Missing cookies skip that platform.

Env: CREATE_TIMEOUT CONNECT_TIMEOUT PROBE_TIMEOUT KICK_TIMEOUT PORT_BASE

Examples:
  $0
  $0 bitrix
  $0 wbstream dc kick
EOF
}

RUN_PLATFORMS=""
RUN_SCENARIOS=""

for _arg in "$@"; do
    case "$_arg" in
        -h | --help)
            usage
            exit 0
            ;;
        telemost | wbstream | dion | bitrix)
            RUN_PLATFORMS="$RUN_PLATFORMS $_arg"
            ;;
        connect | dc | kcp | dual | kick)
            RUN_SCENARIOS="$RUN_SCENARIOS $_arg"
            ;;
        *)
            log "unknown argument: $_arg"
            usage
            exit 2
            ;;
    esac
done

[ -n "$RUN_PLATFORMS" ] || RUN_PLATFORMS="$ALL_PLATFORMS"
[ -n "$RUN_SCENARIOS" ] || RUN_SCENARIOS="$ALL_SCENARIOS"

require_cmd nc
require_cmd ncat

trap cleanup EXIT INT TERM

start_sink
log "sink on 127.0.0.1:$SINK_PORT"

for _pf in $RUN_PLATFORMS; do
    run_platform "$_pf"
    [ "$ABORT_RUN" = "1" ] && break
done

summary
