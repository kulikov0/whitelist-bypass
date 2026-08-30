#!/bin/sh

set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CREATOR="$ROOT/headless/telemost/headless-telemost-creator"
JOINER="$ROOT/headless/telemost-joiner/headless-telemost-joiner"

RATES="${RATES:-0 62500 125000 250000 625000}"
PHASE_SECS="${PHASE_SECS:-100}"
SOCKS_PORT="${SOCKS_PORT:-11080}"
SINK_PORT="${SINK_PORT:-19099}"
SETTLE_TIMEOUT="${SETTLE_TIMEOUT:-60}"

if [ $# -lt 2 ]; then
    echo "Usage: $0 <cookies.json> <telemost-join-link>" >&2
    exit 2
fi
COOKIES="$1"
TM_LINK="$2"

[ -x "$CREATOR" ] || { echo "FAIL: $CREATOR not built (run ./build-headless.sh)" >&2; exit 2; }
[ -x "$JOINER" ]  || { echo "FAIL: $JOINER not built (run ./build-headless.sh)" >&2; exit 2; }
[ -f "$COOKIES" ] || { echo "FAIL: cookies not found: $COOKIES" >&2; exit 2; }
command -v ncat >/dev/null 2>&1 || { echo "FAIL: ncat not found (brew install nmap)" >&2; exit 2; }

CREATOR_LOG=$(mktemp -t tm-reap-creator.XXXXXX.log)
JOINER_LOG=$(mktemp -t tm-reap-joiner.XXXXXX.log)
C_PID=""; J_PID=""; SINK_PID=""

cleanup() {
    [ -n "$SINK_PID" ] && kill "$SINK_PID" 2>/dev/null
    [ -n "$J_PID" ] && kill "$J_PID" 2>/dev/null
    [ -n "$C_PID" ] && kill "$C_PID" 2>/dev/null
    wait 2>/dev/null
}
trap cleanup EXIT INT TERM

echo "room: $TM_LINK"
echo "rates: $RATES  phase: ${PHASE_SECS}s  socks: $SOCKS_PORT  sink: $SINK_PORT"

echo "=== creator joins named room ==="
"$CREATOR" -cookies "$COOKIES" -tm-link "$TM_LINK" > "$CREATOR_LOG" 2>&1 &
C_PID=$!

echo "=== joiner joins named room, exposes SOCKS5 ==="
"$JOINER" -tm-link "$TM_LINK" -socks-port "$SOCKS_PORT" > "$JOINER_LOG" 2>&1 &
J_PID=$!

waited=0
while [ "$waited" -lt "$SETTLE_TIMEOUT" ]; do
    grep -q "TUNNEL CONNECTED" "$JOINER_LOG" && break
    kill -0 "$C_PID" 2>/dev/null || { echo "FAIL: creator died" >&2; tail -20 "$CREATOR_LOG" >&2; exit 1; }
    kill -0 "$J_PID" 2>/dev/null || { echo "FAIL: joiner died" >&2; tail -20 "$JOINER_LOG" >&2; exit 1; }
    sleep 1; waited=$((waited + 1))
done
grep -q "TUNNEL CONNECTED" "$JOINER_LOG" || { echo "FAIL: no TUNNEL CONNECTED in ${SETTLE_TIMEOUT}s" >&2; tail -25 "$JOINER_LOG" >&2; exit 1; }
echo "tunnel up."

# discard sink reached through the tunnel egress; creator and joiner share this host's loopback
ncat -k -l --recv-only 127.0.0.1 "$SINK_PORT" > /dev/null 2>&1 &
SINK_PID=$!
sleep 1

# emit ~rate bytes/sec into stdout for dur seconds, in 10 chunks/sec (coarse token bucket, no pv dep)
pump_rate() {
    rate=$1; dur=$2
    [ "$rate" -le 0 ] && { sleep "$dur"; return; }
    chunk=$((rate / 10)); [ "$chunk" -lt 1 ] && chunk=1
    end=$(( $(date +%s) + dur ))
    while [ "$(date +%s)" -lt "$end" ]; do
        dd if=/dev/zero bs="$chunk" count=1 2>/dev/null
        sleep 0.1
    done
}

# count reap markers currently in both logs
reap_count() {
    c1=$(grep -cE "\[bind\] UNBOUND|forcing reconnect: slot binding killed" "$JOINER_LOG" 2>/dev/null || echo 0)
    c2=$(grep -cE "\[bind\] UNBOUND|forcing reconnect: slot binding killed" "$CREATOR_LOG" 2>/dev/null || echo 0)
    echo $((c1 + c2))
}

echo ""
echo "=== reap sweep ==="
printf "%-10s %-14s %-12s\n" "rate_Bps" "first_reap_s" "reaps_in_phase"
RESULTS=""
for RATE in $RATES; do
    base=$(reap_count)
    start=$(date +%s)
    # drive the load in the background so we can poll for the reap during the phase
    ( pump_rate "$RATE" "$PHASE_SECS" | ncat --proxy 127.0.0.1:"$SOCKS_PORT" --proxy-type socks5 127.0.0.1 "$SINK_PORT" ) >/dev/null 2>&1 &
    LOAD_PID=$!
    first_reap="-"
    while kill -0 "$LOAD_PID" 2>/dev/null; do
        if [ "$first_reap" = "-" ] && [ "$(reap_count)" -gt "$base" ]; then
            first_reap=$(( $(date +%s) - start ))
        fi
        sleep 1
    done
    wait "$LOAD_PID" 2>/dev/null
    # one final check after the phase ends
    if [ "$first_reap" = "-" ] && [ "$(reap_count)" -gt "$base" ]; then
        first_reap=$(( $(date +%s) - start ))
    fi
    total=$(( $(reap_count) - base ))
    printf "%-10s %-14s %-12s\n" "$RATE" "$first_reap" "$total"
    RESULTS="$RESULTS$RATE=$first_reap,${total} "
done

echo ""
echo "--- joiner reap lines ---"
grep -nE "\[bind\] (BOUND|UNBOUND)|forcing reconnect" "$JOINER_LOG" | tail -20
echo ""
echo "summary (rate_Bps=first_reap_s,reaps): $RESULTS"
echo "logs: creator=$CREATOR_LOG joiner=$JOINER_LOG"
