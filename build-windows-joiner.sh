#!/bin/sh
set -e

# Builds the Windows joiner Go binary for x64, arm64 and ia32, then
# packages it into the Electron app under joiner-desktop-app/.
#
# Outputs in $ROOT/prebuilts/:
#   windows-joiner-x64.exe
#   windows-joiner-arm64.exe
#   windows-joiner-ia32.exe
#   wintun/<arch>/wintun.dll
#   WhitelistBypass Joiner-<version>-<arch>.exe (installer, when NPM_BUILD=1)
#
# The wintun.dll files are fetched from wintun.net the first time.
# Side-loading them next to the .exe is how the wireguard-go runtime
# locates the driver.

ROOT="$(cd "$(dirname "$0")" && pwd)"
PREBUILTS="$ROOT/prebuilts"
JOINER_GO_DIR="$ROOT/joiner-desktop-app/windows-joiner"
ELECTRON_DIR="$ROOT/joiner-desktop-app"
WINTUN_DIR="$PREBUILTS/wintun"
WINTUN_VERSION="0.14.1"
WINTUN_URL="https://www.wintun.net/builds/wintun-${WINTUN_VERSION}.zip"

mkdir -p "$PREBUILTS" "$WINTUN_DIR"

build_arch() {
    GOARCH_GO="$1"
    OUT_TAG="$2"
    WINTUN_ARCH="$3"
    echo ""
    echo "=== Building windows-joiner ($OUT_TAG / GOARCH=$GOARCH_GO) ==="
    cd "$JOINER_GO_DIR"
    GOOS=windows GOARCH="$GOARCH_GO" go build \
        -trimpath -ldflags="-s -w" \
        -o "$PREBUILTS/windows-joiner-$OUT_TAG.exe" .
    ls -lh "$PREBUILTS/windows-joiner-$OUT_TAG.exe"

    mkdir -p "$WINTUN_DIR/$OUT_TAG"
    if [ ! -f "$WINTUN_DIR/$OUT_TAG/wintun.dll" ]; then
        if [ ! -f "$WINTUN_DIR/wintun.zip" ]; then
            echo "[wintun] downloading $WINTUN_URL"
            curl -L -o "$WINTUN_DIR/wintun.zip" "$WINTUN_URL"
        fi
        echo "[wintun] extracting $WINTUN_ARCH"
        ( cd "$WINTUN_DIR" && unzip -o -j wintun.zip "wintun/bin/$WINTUN_ARCH/wintun.dll" \
            -d "$OUT_TAG" >/dev/null )
    fi
    ls -lh "$WINTUN_DIR/$OUT_TAG/wintun.dll"
}

build_arch amd64 x64   amd64
build_arch arm64 arm64 arm64
build_arch 386   ia32  x86

if [ "${NPM_BUILD:-0}" = "1" ]; then
    echo ""
    echo "=== Packaging Electron joiner-desktop-app ==="
    cd "$ELECTRON_DIR"
    if [ ! -d node_modules/typescript ]; then
        echo "[npm] installing dev deps"
        npm install
    fi
    npx tsc
    npx electron-builder --win --x64 --ia32 --arm64 --publish never
fi

echo ""
echo "=== Done ==="
ls -lh "$PREBUILTS"/windows-joiner-*.exe
