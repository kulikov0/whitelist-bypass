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

Platforms: $ALL_PLATFORMS vk
Scenarios: $ALL_SCENARIOS

No args runs every applicable scenario on every platform, except vk.

See headless/tests/README.md.
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
        vk | telemost | wbstream | dion | bitrix)
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
log "sink on $SINK_BIND:$SINK_PORT"

for _pf in $RUN_PLATFORMS; do
    run_platform "$_pf"
    [ "$ABORT_RUN" = "1" ] && break
done

summary
