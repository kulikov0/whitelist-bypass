#!/bin/sh
set -e

# Builds the user-facing Electron joiner app for Windows (portable .exe)
# and Linux (AppImage), following the same per-arch bundle pattern as
# build-creator.sh. Output ends up in prebuilts/ via electron-builder's
# directories.output.

ROOT="$(cd "$(dirname "$0")" && pwd)"
JOINER_GO_DIR="$ROOT/joiner-desktop-app/desktop-joiner"
ELECTRON_DIR="$ROOT/joiner-desktop-app"

echo "=== Building Go backend ==="
"$ROOT/build-desktop-joiner.sh"

cd "$ELECTRON_DIR"
if [ ! -d node_modules/typescript ]; then
    echo "[npm] installing dev deps"
    npm install
fi
npx tsc

cleanup_artifacts() {
    rm -f "$JOINER_GO_DIR"/desktop-joiner-windows-*.exe \
          "$JOINER_GO_DIR"/desktop-joiner-linux-* \
          "$JOINER_GO_DIR"/desktop-joiner-darwin \
          "$JOINER_GO_DIR"/desktop-joiner-bundle \
          "$JOINER_GO_DIR"/desktop-joiner-bundle.exe \
          "$JOINER_GO_DIR"/wintun-*.dll
}
trap cleanup_artifacts EXIT

echo ""
echo "--- Windows x64 ---"
cp "$JOINER_GO_DIR/desktop-joiner-windows-x64.exe" "$JOINER_GO_DIR/desktop-joiner-bundle.exe"
cp "$JOINER_GO_DIR/wintun-x64.dll" "$JOINER_GO_DIR/wintun-bundle.dll"
npx electron-builder --win --x64 --publish never

echo ""
echo "--- Windows x86 ---"
cp "$JOINER_GO_DIR/desktop-joiner-windows-ia32.exe" "$JOINER_GO_DIR/desktop-joiner-bundle.exe"
cp "$JOINER_GO_DIR/wintun-ia32.dll" "$JOINER_GO_DIR/wintun-bundle.dll"
npx electron-builder --win --ia32 --publish never

echo ""
echo "--- Linux x64 ---"
cp "$JOINER_GO_DIR/desktop-joiner-linux-x64" "$JOINER_GO_DIR/desktop-joiner-bundle"
chmod +x "$JOINER_GO_DIR/desktop-joiner-bundle"
npx electron-builder --linux --x64 --publish never

if [ "$(uname)" = "Darwin" ]; then
    echo ""
    echo "--- macOS (universal) ---"
    npx electron-builder --mac --publish never || true
fi

"$ROOT/clean-prebuilts.sh"

echo ""
echo "=== Done ==="
ls -lh "$ROOT/prebuilts"/WhitelistBypass* 2>/dev/null || true
