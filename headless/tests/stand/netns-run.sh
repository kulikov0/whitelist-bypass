#!/bin/sh
set -u

DIR="$(cd "$(dirname "$0")" && pwd)"
E2E_ROOT="${E2E_ROOT:-/work}"
HEADLESS="$E2E_ROOT/headless"
E2E_DIR="$HEADLESS/tests"

[ -f "$E2E_DIR/.env" ] && . "$E2E_DIR/.env"

. "$E2E_DIR/lib.sh"
. "$E2E_DIR/platforms.sh"
. "$E2E_DIR/scenarios.sh"
. "$DIR/netns.sh"

CREATE_TIMEOUT="${CREATE_TIMEOUT:-45}"
CONNECT_TIMEOUT="${CONNECT_TIMEOUT:-45}"
PROBE_TIMEOUT="${PROBE_TIMEOUT:-25}"
KICK_TIMEOUT="${KICK_TIMEOUT:-30}"

ALL_PLATFORMS="telemost wbstream dion bitrix"
ALL_SCENARIOS="connect dc kcp dual kick"

RUN_PLATFORMS=""
RUN_SCENARIOS=""
for _arg in "$@"; do
    case "$_arg" in
        telemost | wbstream | dion | bitrix) RUN_PLATFORMS="$RUN_PLATFORMS $_arg" ;;
        connect | dc | kcp | dual | kick) RUN_SCENARIOS="$RUN_SCENARIOS $_arg" ;;
        *)
            log "unknown argument: $_arg"
            exit 2
            ;;
    esac
done
[ -n "$RUN_PLATFORMS" ] || RUN_PLATFORMS="$ALL_PLATFORMS"
[ -n "$RUN_SCENARIOS" ] || RUN_SCENARIOS="$ALL_SCENARIOS"

require_cmd nc
require_cmd ncat
require_cmd ip
require_cmd iptables

CREATOR_WRAP="ip netns exec creator"
JOINER_WRAP="ip netns exec joiner"
JOINER_SOCKS_HOST="0.0.0.0"
PROBE_HOST="10.201.2.2"
SINK_WRAP="ip netns exec creator"
SINK_BIND="127.0.0.1"
SINK_TARGET="127.0.0.1"

cleanup_all() {
    cleanup
    teardown_ns
}
trap cleanup_all EXIT INT TERM

setup_ns creator 1
setup_ns joiner 2
start_sink
log "netns creator=10.201.1.2 joiner=10.201.2.2 sink=creator:$SINK_PORT"

for _pf in $RUN_PLATFORMS; do
    run_platform "$_pf"
    [ "$ABORT_RUN" = "1" ] && break
done

summary
