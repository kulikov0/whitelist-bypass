#!/bin/sh
set -e

# Builds the Windows joiner Go binary and fetches wintun.dll, next to
# the Go source in joiner-desktop-app/windows-joiner/. Nothing is
# written to prebuilts/. Run ./build-joiner-app.sh afterwards to
# produce the Electron installer.

ROOT="$(cd "$(dirname "$0")" && pwd)"
JOINER_GO_DIR="$ROOT/joiner-desktop-app/windows-joiner"
WINTUN_VERSION="0.14.1"
WINTUN_URL="https://www.wintun.net/builds/wintun-${WINTUN_VERSION}.zip"

build_arch() {
    GOARCH_GO="$1"
    OUT_TAG="$2"
    WINTUN_ARCH="$3"
    echo ""
    echo "=== Building windows-joiner ($OUT_TAG / GOARCH=$GOARCH_GO) ==="
    cd "$JOINER_GO_DIR"
    GOOS=windows GOARCH="$GOARCH_GO" go build \
        -trimpath -ldflags="-s -w" \
        -o "$JOINER_GO_DIR/windows-joiner-$OUT_TAG.exe" .
    ls -lh "$JOINER_GO_DIR/windows-joiner-$OUT_TAG.exe"

    if [ ! -f "$JOINER_GO_DIR/wintun-$OUT_TAG.dll" ]; then
        if [ ! -f "$JOINER_GO_DIR/wintun.zip" ]; then
            echo "[wintun] downloading $WINTUN_URL"
            curl -L -o "$JOINER_GO_DIR/wintun.zip" "$WINTUN_URL"
        fi
        echo "[wintun] extracting $WINTUN_ARCH"
        unzip -o -j "$JOINER_GO_DIR/wintun.zip" "wintun/bin/$WINTUN_ARCH/wintun.dll" \
            -d "$JOINER_GO_DIR" >/dev/null
        mv "$JOINER_GO_DIR/wintun.dll" "$JOINER_GO_DIR/wintun-$OUT_TAG.dll"
    fi
    ls -lh "$JOINER_GO_DIR/wintun-$OUT_TAG.dll"
}

build_arch amd64 x64   amd64
build_arch arm64 arm64 arm64
build_arch 386   ia32  x86

rm -f "$JOINER_GO_DIR/wintun.zip"

echo ""
echo "=== Done ==="
ls -lh "$JOINER_GO_DIR"/windows-joiner-*.exe "$JOINER_GO_DIR"/wintun-*.dll
