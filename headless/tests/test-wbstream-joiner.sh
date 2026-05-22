#!/bin/sh
# End-to-end smoke test: spawn a wbstream creator + joiner pair, route curl
# through the joiner's SOCKS5 and verify the response arrives.
#
# Usage:
#   ./test-wbstream.sh [video|dc]            # default: video

set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CREATOR="$ROOT/headless/wbstream/headless-wbstream-creator"
JOINER="$ROOT/headless/wbstream-joiner/headless-wbstream-joiner"
SOCKS_PORT=11080
TARGET_URL="https://api.ipify.org"
PROBE_TIMEOUT=15
SETTLE_TIMEOUT=20

mode_arg="${1:-video}"
case "$mode_arg" in
    video|dc) ;;
    *)
        echo "Unknown mode: $mode_arg (expected video or dc)" >&2
        exit 2
        ;;
esac

CREATOR_LOG=$(mktemp -t wb-c.XXXXXX.log)
JOINER_LOG=$(mktemp -t wb-j.XXXXXX.log)
CREATOR_PID=""
JOINER_PID=""

cleanup() {
    [ -n "$JOINER_PID" ] && kill "$JOINER_PID" 2>/dev/null
    [ -n "$CREATOR_PID" ] && kill "$CREATOR_PID" 2>/dev/null
    wait 2>/dev/null
}
trap cleanup EXIT INT TERM

require_bin() {
    if [ ! -x "$1" ]; then
        echo "FAIL: missing binary $1" >&2
        echo "Run ./build-headless.sh first." >&2
        exit 2
    fi
}

require_bin "$CREATOR"
require_bin "$JOINER"

echo "=== mode=$mode_arg ==="

"$CREATOR" > "$CREATOR_LOG" 2>&1 &
CREATOR_PID=$!

waited=0
JOIN_LINK=""
while [ "$waited" -lt "$SETTLE_TIMEOUT" ]; do
    JOIN_LINK=$(grep -m1 "join_link:" "$CREATOR_LOG" | awk '{print $2}')
    [ -n "$JOIN_LINK" ] && break
    sleep 1
    waited=$((waited + 1))
done

if [ -z "$JOIN_LINK" ]; then
    echo "FAIL: creator did not print a join_link within ${SETTLE_TIMEOUT}s" >&2
    tail -20 "$CREATOR_LOG" >&2
    exit 1
fi
echo "join_link=$JOIN_LINK"

"$JOINER" --room "$JOIN_LINK" --socks-port "$SOCKS_PORT" --tunnel-mode "$mode_arg" \
    > "$JOINER_LOG" 2>&1 &
JOINER_PID=$!

waited=0
while [ "$waited" -lt "$SETTLE_TIMEOUT" ]; do
    if grep -q "TUNNEL CONNECTED" "$JOINER_LOG"; then
        break
    fi
    sleep 1
    waited=$((waited + 1))
done

if ! grep -q "TUNNEL CONNECTED" "$JOINER_LOG"; then
    echo "FAIL: joiner did not reach TUNNEL CONNECTED within ${SETTLE_TIMEOUT}s" >&2
    tail -20 "$JOINER_LOG" >&2
    exit 1
fi

# SOCKS listener starts in a goroutine after TUNNEL CONNECTED prints; poll.
waited=0
while [ "$waited" -lt 10 ]; do
    if nc -z 127.0.0.1 "$SOCKS_PORT" 2>/dev/null; then
        break
    fi
    sleep 1
    waited=$((waited + 1))
done
if ! nc -z 127.0.0.1 "$SOCKS_PORT" 2>/dev/null; then
    echo "FAIL: SOCKS5 port $SOCKS_PORT never opened" >&2
    exit 1
fi

response=$(curl --socks5 "127.0.0.1:$SOCKS_PORT" -m "$PROBE_TIMEOUT" -sv "$TARGET_URL" 2>&1)
if ! echo "$response" | grep -q "HTTP/.* 200"; then
    echo "FAIL: SOCKS5 request did not return HTTP 200" >&2
    echo "--- curl output ---" >&2
    echo "$response" >&2
    echo "--- joiner log tail ---" >&2
    tail -30 "$JOINER_LOG" >&2
    echo "--- creator log tail ---" >&2
    tail -30 "$CREATOR_LOG" >&2
    exit 1
fi
response=$(echo "$response" | tail -1)
echo "remote_ip=$response"

speed=$(curl --socks5 "127.0.0.1:$SOCKS_PORT" -m 30 -s -o /dev/null \
    -w "%{speed_download}" \
    "https://speed.cloudflare.com/__down?bytes=10485760")
if [ -z "$speed" ] || [ "$speed" = "0" ]; then
    echo "FAIL: throughput probe returned 0 B/s" >&2
    exit 1
fi
mbps=$(awk -v b="$speed" 'BEGIN { printf "%.2f", b * 8 / 1000000 }')
echo "throughput=${mbps} Mbps"

echo "PASS: mode=$mode_arg"
