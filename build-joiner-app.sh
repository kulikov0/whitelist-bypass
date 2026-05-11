#!/bin/sh
set -e

# Builds the user-facing Electron joiner app for Windows, following the
# same per-arch bundle pattern as build-creator.sh. Output is the NSIS
# installer in prebuilts/ (via electron-builder's directories.output).

ROOT="$(cd "$(dirname "$0")" && pwd)"
JOINER_GO_DIR="$ROOT/joiner-desktop-app/windows-joiner"
ELECTRON_DIR="$ROOT/joiner-desktop-app"

echo "=== Building Go backend ==="
"$ROOT/build-windows-joiner.sh"

cd "$ELECTRON_DIR"
if [ ! -d node_modules/typescript ]; then
    echo "[npm] installing dev deps"
    npm install
fi
npx tsc

cleanup_bundle() {
    rm -f "$JOINER_GO_DIR/windows-joiner-bundle.exe" \
          "$JOINER_GO_DIR/wintun-bundle.dll"
}
trap cleanup_bundle EXIT

for pair in "x64 --x64" "ia32 --ia32" "arm64 --arm64"; do
    arch="${pair% *}"
    flag="${pair#* }"
    echo ""
    echo "--- Windows $arch ---"
    cp "$JOINER_GO_DIR/windows-joiner-$arch.exe" "$JOINER_GO_DIR/windows-joiner-bundle.exe"
    cp "$JOINER_GO_DIR/wintun-$arch.dll" "$JOINER_GO_DIR/wintun-bundle.dll"
    npx electron-builder --win $flag --publish never
done

echo ""
echo "=== Done ==="
ls -lh "$ROOT/prebuilts"/WhitelistBypass*.exe 2>/dev/null || true
