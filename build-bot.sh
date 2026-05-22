#!/bin/sh
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
BOT_DIR="$ROOT/headless/vk-bot"
PREBUILTS="$ROOT/prebuilts"

mkdir -p "$PREBUILTS"

echo "=== Building headless-vk-bot (Linux) ==="
cd "$BOT_DIR"
echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$PREBUILTS/headless-vk-bot-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$PREBUILTS/headless-vk-bot-linux-ia32" .

echo ""
echo "=== Done ==="
ls -lh "$PREBUILTS"/headless-vk-bot-linux-*
