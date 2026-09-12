#!/bin/sh
set -e

# Builds the user-facing Electron joiner app for Windows (portable .exe)
# and Linux (AppImage), following the same per-arch bundle pattern as
# build-creator.sh. Output ends up in prebuilts/ via electron-builder's
# directories.output.

ROOT="$(cd "$(dirname "$0")" && pwd)"
JOINER_GO_DIR="$ROOT/joiner-desktop-app/desktop-joiner"
ELECTRON_DIR="$ROOT/joiner-desktop-app"
PREBUILTS="$ROOT/prebuilts"

verify_artifact() {
    artifact_name="$1"
    if [ ! -f "$PREBUILTS/$artifact_name" ]; then
        echo "packaging failed, $artifact_name is missing from prebuilts"
        exit 1
    fi
    echo "packaged $artifact_name $(du -h "$PREBUILTS/$artifact_name" | cut -f1)"
}

echo "=== Building Go backend ==="
"$ROOT/build-desktop-joiner.sh"

cd "$ELECTRON_DIR"
if [ ! -d node_modules/typescript ]; then
    echo "[npm] installing dev deps"
    npm install
fi
npx tsc

PRODUCT_NAME="$(node -p "require('$ELECTRON_DIR/package.json').build.productName")"
APP_VERSION="$(node -p "require('$ELECTRON_DIR/package.json').version")"

cleanup_artifacts() {
    rm -f "$JOINER_GO_DIR"/desktop-joiner-windows-*.exe \
          "$JOINER_GO_DIR"/desktop-joiner-linux-* \
          "$JOINER_GO_DIR"/desktop-joiner-darwin* \
          "$JOINER_GO_DIR"/desktop-joiner-bundle \
          "$JOINER_GO_DIR"/desktop-joiner-bundle.exe \
          "$JOINER_GO_DIR"/wintun-*.dll
}
trap cleanup_artifacts EXIT

echo ""
echo "--- Windows x64 ---"
cp "$JOINER_GO_DIR/desktop-joiner-windows-x64.exe" "$JOINER_GO_DIR/desktop-joiner-bundle.exe"
cp "$JOINER_GO_DIR/wintun-x64.dll" "$JOINER_GO_DIR/wintun-bundle.dll"
rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-x64.exe"
npx electron-builder --win --x64 --publish never
verify_artifact "$PRODUCT_NAME-$APP_VERSION-x64.exe"

echo ""
echo "--- Windows x86 ---"
cp "$JOINER_GO_DIR/desktop-joiner-windows-ia32.exe" "$JOINER_GO_DIR/desktop-joiner-bundle.exe"
cp "$JOINER_GO_DIR/wintun-ia32.dll" "$JOINER_GO_DIR/wintun-bundle.dll"
rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-ia32.exe"
npx electron-builder --win --ia32 --publish never
verify_artifact "$PRODUCT_NAME-$APP_VERSION-ia32.exe"

echo ""
echo "--- Linux x64 ---"
cp "$JOINER_GO_DIR/desktop-joiner-linux-x64" "$JOINER_GO_DIR/desktop-joiner-bundle"
chmod +x "$JOINER_GO_DIR/desktop-joiner-bundle"
rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-x86_64.AppImage"
npx electron-builder --linux --x64 --publish never
verify_artifact "$PRODUCT_NAME-$APP_VERSION-x86_64.AppImage"

if [ "$(uname)" = "Darwin" ]; then
    echo ""
    echo "--- macOS x64 ---"
    cp "$JOINER_GO_DIR/desktop-joiner-darwin-amd64" "$JOINER_GO_DIR/desktop-joiner-darwin"
    rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-x64.dmg"
    npx electron-builder --mac --x64 --publish never
    verify_artifact "$PRODUCT_NAME-$APP_VERSION-x64.dmg"

    echo ""
    echo "--- macOS arm64 ---"
    cp "$JOINER_GO_DIR/desktop-joiner-darwin-arm64" "$JOINER_GO_DIR/desktop-joiner-darwin"
    rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-arm64.dmg"
    npx electron-builder --mac --arm64 --publish never
    verify_artifact "$PRODUCT_NAME-$APP_VERSION-arm64.dmg"
fi

"$ROOT/clean-prebuilts.sh"

echo ""
echo "=== Done ==="
ls -lh "$ROOT/prebuilts"/WhitelistBypass* 2>/dev/null || true
