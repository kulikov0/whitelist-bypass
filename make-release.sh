#!/bin/sh
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
PREBUILTS="$ROOT/prebuilts"

mkdir -p "$PREBUILTS"

echo "=== Building Android app ==="
"$ROOT/build-go.sh"
"$ROOT/build-app.sh"

echo ""
echo "=== Building creator-app + headless creators ==="
"$ROOT/build-creator.sh"

echo ""
echo "=== Building Linux headless joiners ==="
"$ROOT/build-joiners.sh"

echo ""
echo "=== Building Windows joiner (Go binary + wintun.dll) ==="
"$ROOT/build-windows-joiner.sh"

echo ""
echo "=== Building Linux headless-vk-bot ==="
"$ROOT/build-bot.sh"

if [ "$(uname)" = "Darwin" ]; then
    echo ""
    echo "=== Building iOS app ==="
    "$ROOT/build-ios.sh"
else
    echo ""
    echo "=== Skipping iOS build (requires macOS) ==="
fi

echo ""
echo "=== Release complete ==="
ls -lh "$PREBUILTS/"
