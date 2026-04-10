#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"

echo "Building headless-vk-creator..."
go -C "$ROOT/headless/vk" build -ldflags="-s -w" -o headless-vk-creator .

echo "Building headless-telemost-creator..."
go -C "$ROOT/headless/telemost" build -ldflags="-s -w" -o headless-telemost-creator .

echo "Done."
ls -lh "$ROOT/headless/vk/headless-vk-creator" "$ROOT/headless/telemost/headless-telemost-creator"
