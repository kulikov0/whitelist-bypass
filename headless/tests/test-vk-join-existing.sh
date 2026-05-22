#!/bin/sh
# Smoke test for -vk-link: create a call, then join it via the new flag.
#
# Usage: ./test-vk-join-existing.sh <path-to-cookies.json>

set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CREATOR="$ROOT/headless/vk/headless-vk-creator"
if [ $# -lt 1 ]; then
    echo "Usage: $0 <path-to-cookies.json>" >&2
    exit 2
fi
COOKIES="$1"
SETTLE_TIMEOUT=25

[ -x "$CREATOR" ] || { echo "FAIL: $CREATOR not built, run ./build-headless.sh" >&2; exit 2; }
[ -f "$COOKIES" ] || { echo "FAIL: cookies not found at: $COOKIES" >&2; exit 2; }
echo "cookies: $COOKIES"

PHASE1_LOG=$(mktemp -t vk-create.XXXXXX.log)
PHASE2_LOG=$(mktemp -t vk-join.XXXXXX.log)
P1_PID=""
P2_PID=""

cleanup() {
    [ -n "$P2_PID" ] && kill "$P2_PID" 2>/dev/null
    [ -n "$P1_PID" ] && kill "$P1_PID" 2>/dev/null
    wait 2>/dev/null
}
trap cleanup EXIT INT TERM

echo "=== phase 1: create call ==="
"$CREATOR" -cookies "$COOKIES" > "$PHASE1_LOG" 2>&1 &
P1_PID=$!

waited=0
JOIN_LINK=""
while [ "$waited" -lt "$SETTLE_TIMEOUT" ]; do
    JOIN_LINK=$(grep -m1 -oE "https://vk\.com/call/join/[A-Za-z0-9_-]+" "$PHASE1_LOG")
    [ -n "$JOIN_LINK" ] && break
    if ! kill -0 "$P1_PID" 2>/dev/null; then
        echo "FAIL: phase 1 process died" >&2
        tail -20 "$PHASE1_LOG" >&2
        exit 1
    fi
    sleep 1
    waited=$((waited + 1))
done

if [ -z "$JOIN_LINK" ]; then
    echo "FAIL: phase 1 did not print join_link within ${SETTLE_TIMEOUT}s" >&2
    tail -20 "$PHASE1_LOG" >&2
    exit 1
fi
echo "phase 1 join_link: $JOIN_LINK"

# Wait a beat so the call is fully joined on VK's side before phase 2 starts.
sleep 3

echo ""
echo "=== phase 2: join existing call via -vk-link ==="
"$CREATOR" -cookies "$COOKIES" -vk-link "$JOIN_LINK" > "$PHASE2_LOG" 2>&1 &
P2_PID=$!

waited=0
while [ "$waited" -lt "$SETTLE_TIMEOUT" ]; do
    if grep -q "vk-ws] Connected" "$PHASE2_LOG"; then
        break
    fi
    if grep -qE "Failed to join existing call|empty WS endpoint|empty session_key" "$PHASE2_LOG"; then
        break
    fi
    if ! kill -0 "$P2_PID" 2>/dev/null; then
        break
    fi
    sleep 1
    waited=$((waited + 1))
done

echo ""
echo "--- phase 2 log tail ---"
tail -25 "$PHASE2_LOG"

if grep -q "vk-ws] Connected" "$PHASE2_LOG"; then
    echo ""
    echo "PASS: phase 2 joined the existing call (got vk-ws connection)"
    exit 0
fi

echo ""
echo "FAIL: phase 2 did not reach vk-ws connection" >&2
exit 1
