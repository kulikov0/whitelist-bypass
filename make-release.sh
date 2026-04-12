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
echo "=== Release complete ==="
ls -lh "$PREBUILTS/"
