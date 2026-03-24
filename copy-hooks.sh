#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
ASSETS="$ROOT/android-app/app/src/main/assets"
mkdir -p "$ASSETS"
cp "$ROOT/hooks/dc-joiner-vk.js" "$ASSETS/dc-joiner-vk.js"
cp "$ROOT/hooks/dc-joiner-telemost.js" "$ASSETS/dc-joiner-telemost.js"
cp "$ROOT/hooks/video-vk.js" "$ASSETS/video-vk.js"
cp "$ROOT/hooks/video-telemost.js" "$ASSETS/video-telemost.js"
echo "Hooks copied to assets"
