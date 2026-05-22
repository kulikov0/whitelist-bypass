#!/bin/sh
# Smoke test for headless-wbstream-creator -room: create a room, then a second
# creator joins the same room id.

set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CREATOR="$ROOT/headless/wbstream/headless-wbstream-creator"
SETTLE_TIMEOUT=25

[ -x "$CREATOR" ] || { echo "FAIL: $CREATOR not built (run ./build-headless.sh)" >&2; exit 2; }

PHASE1_LOG=$(mktemp -t wb-create.XXXXXX.log)
PHASE2_LOG=$(mktemp -t wb-join.XXXXXX.log)
P1_PID=""
P2_PID=""

cleanup() {
    [ -n "$P2_PID" ] && kill "$P2_PID" 2>/dev/null
    [ -n "$P1_PID" ] && kill "$P1_PID" 2>/dev/null
    wait 2>/dev/null
}
trap cleanup EXIT INT TERM

echo "=== phase 1: create room ==="
"$CREATOR" > "$PHASE1_LOG" 2>&1 &
P1_PID=$!

waited=0
JOIN_LINK=""
while [ "$waited" -lt "$SETTLE_TIMEOUT" ]; do
    JOIN_LINK=$(grep -m1 -oE "wbstream://[A-Za-z0-9-]+" "$PHASE1_LOG" | head -1)
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

sleep 3

echo ""
echo "=== phase 2: join existing room via -room ==="
"$CREATOR" -room "$JOIN_LINK" > "$PHASE2_LOG" 2>&1 &
P2_PID=$!

waited=0
while [ "$waited" -lt "$SETTLE_TIMEOUT" ]; do
    if grep -q "\[lk\] join: room=" "$PHASE2_LOG"; then
        break
    fi
    if grep -qE "Failed to|register guest|create room" "$PHASE2_LOG" | grep -qiE "error|failed"; then
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

phase2_room=$(grep -m1 -oE "room=[a-f0-9-]+" "$PHASE2_LOG" | head -1 | cut -d= -f2)
expected_room=$(echo "$JOIN_LINK" | sed 's|wbstream://||')

if [ "$phase2_room" = "$expected_room" ]; then
    echo ""
    echo "PASS: phase 2 joined room $phase2_room"
    exit 0
fi

echo ""
echo "FAIL: phase 2 did not join the expected room (got '$phase2_room', wanted '$expected_room')" >&2
exit 1
