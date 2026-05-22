#!/bin/sh
set -e

# Builds the desktop-joiner Go binary for Windows and Linux, plus
# fetches wintun.dll for Windows. Output lives next to the Go source
# in joiner-desktop-app/desktop-joiner/. Nothing is written to
# prebuilts/. Run ./build-joiner-app.sh afterwards to produce the
# Electron installers.

ROOT="$(cd "$(dirname "$0")" && pwd)"
JOINER_GO_DIR="$ROOT/joiner-desktop-app/desktop-joiner"
WINTUN_VERSION="0.14.1"
WINTUN_URL="https://www.wintun.net/builds/wintun-${WINTUN_VERSION}.zip"

build_windows() {
    GOARCH_GO="$1"
    OUT_TAG="$2"
    WINTUN_ARCH="$3"
    echo ""
    echo "=== Building desktop-joiner windows-$OUT_TAG (GOARCH=$GOARCH_GO) ==="
    cd "$JOINER_GO_DIR"
    GOOS=windows GOARCH="$GOARCH_GO" go build \
        -trimpath -ldflags="-s -w" \
        -o "$JOINER_GO_DIR/desktop-joiner-windows-$OUT_TAG.exe" .
    ls -lh "$JOINER_GO_DIR/desktop-joiner-windows-$OUT_TAG.exe"

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

build_linux() {
    GOARCH_GO="$1"
    OUT_TAG="$2"
    echo ""
    echo "=== Building desktop-joiner linux-$OUT_TAG (GOARCH=$GOARCH_GO) ==="
    cd "$JOINER_GO_DIR"
    GOOS=linux GOARCH="$GOARCH_GO" go build \
        -trimpath -ldflags="-s -w" \
        -o "$JOINER_GO_DIR/desktop-joiner-linux-$OUT_TAG" .
    ls -lh "$JOINER_GO_DIR/desktop-joiner-linux-$OUT_TAG"
}

build_darwin_universal() {
    echo ""
    echo "=== Building desktop-joiner darwin (universal) ==="
    cd "$JOINER_GO_DIR"
    GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o desktop-joiner-darwin-amd64 .
    GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o desktop-joiner-darwin-arm64 .
    lipo -create -output desktop-joiner-darwin desktop-joiner-darwin-amd64 desktop-joiner-darwin-arm64
    rm desktop-joiner-darwin-amd64 desktop-joiner-darwin-arm64
    ls -lh "$JOINER_GO_DIR/desktop-joiner-darwin"
}

build_windows amd64 x64  amd64
build_windows 386   ia32 x86

build_linux amd64 x64

build_darwin_universal

rm -f "$JOINER_GO_DIR/wintun.zip"

echo ""
echo "=== Done ==="
ls -lh "$JOINER_GO_DIR"/desktop-joiner-* "$JOINER_GO_DIR"/wintun-*.dll
