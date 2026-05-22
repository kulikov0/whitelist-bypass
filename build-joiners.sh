#!/bin/sh
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
HEADLESS_DIR="$ROOT/headless"
WB_JOINER_DIR="$HEADLESS_DIR/wbstream-joiner"
TM_JOINER_DIR="$HEADLESS_DIR/telemost-joiner"
DION_JOINER_DIR="$HEADLESS_DIR/dion-joiner"
PREBUILTS="$ROOT/prebuilts"

mkdir -p "$PREBUILTS"

echo "=== Building headless-wbstream-joiner (Linux) ==="
cd "$WB_JOINER_DIR"
echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$PREBUILTS/headless-wbstream-joiner-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$PREBUILTS/headless-wbstream-joiner-linux-ia32" .

echo ""
echo "=== Building headless-telemost-joiner (Linux) ==="
cd "$TM_JOINER_DIR"
echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$PREBUILTS/headless-telemost-joiner-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$PREBUILTS/headless-telemost-joiner-linux-ia32" .

echo ""
echo "=== Building headless-dion-joiner (Linux) ==="
cd "$DION_JOINER_DIR"
echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$PREBUILTS/headless-dion-joiner-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$PREBUILTS/headless-dion-joiner-linux-ia32" .

echo ""
echo "=== Done ==="
ls -lh "$PREBUILTS"/headless-*-joiner-linux-*
