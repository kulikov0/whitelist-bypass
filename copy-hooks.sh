#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
ASSETS="$ROOT/android-app/app/src/main/assets"
mkdir -p "$ASSETS"
cp "$ROOT/hooks/dc-joiner-vk.js" "$ASSETS/dc-joiner-vk.js"
cp "$ROOT/hooks/dc-joiner-telemost.js" "$ASSETS/dc-joiner-telemost.js"
cp "$ROOT/hooks/video-vk.js" "$ASSETS/video-vk.js"
cp "$ROOT/hooks/video-telemost.js" "$ASSETS/video-telemost.js"
cp "$ROOT/hooks/autoclick-telemost.js" "$ASSETS/autoclick-telemost.js"
cp "$ROOT/hooks/autoclick-vk.js" "$ASSETS/autoclick-vk.js"
cp "$ROOT/hooks/mute-audio-context.js" "$ASSETS/mute-audio-context.js"
cp "$ROOT/hooks/headless-auth-vk.js" "$ASSETS/headless-auth-vk.js"
echo "Hooks copied to assets"
