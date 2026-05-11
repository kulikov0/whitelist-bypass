#!/bin/sh
set -e

# Builds the user-facing Electron joiner app for Windows.
# Output: prebuilts/WhitelistBypass Joiner-<version>-<arch>.exe (NSIS installer).
#
# Run build-windows-joiner.sh first to produce the Go backend binaries
# and wintun.dll; this script packages them into the Electron installer.

ROOT="$(cd "$(dirname "$0")" && pwd)"
PREBUILTS="$ROOT/prebuilts"
ELECTRON_DIR="$ROOT/joiner-desktop-app"

need_backend=0
for arch in x64 arm64 ia32; do
    if [ ! -f "$PREBUILTS/windows-joiner-$arch.exe" ] || \
       [ ! -f "$PREBUILTS/wintun/$arch/wintun.dll" ]; then
        need_backend=1
        break
    fi
done

if [ "$need_backend" = "1" ]; then
    echo "=== Building Go backend first ==="
    "$ROOT/build-windows-joiner.sh"
fi

echo "=== Packaging Electron joiner-desktop-app ==="
cd "$ELECTRON_DIR"
if [ ! -d node_modules/typescript ]; then
    echo "[npm] installing dev deps"
    npm install
fi
npx tsc
npx electron-builder --win --x64 --ia32 --arm64 --publish never

echo ""
echo "=== Done ==="
ls -lh "$PREBUILTS"/WhitelistBypass*.exe 2>/dev/null || true
