#!/bin/sh
set -e

export ANDROID_HOME="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$(ls -d $ANDROID_HOME/ndk/* 2>/dev/null | tail -1)}"
export CGO_LDFLAGS="-Wl,-z,max-page-size=16384"
export PATH="$PATH:/opt/homebrew/bin:$HOME/go/bin"

# Check deps
command -v go >/dev/null || { echo "go not found"; exit 1; }
command -v gomobile >/dev/null || { echo "gomobile not found, run: go install golang.org/x/mobile/cmd/gomobile@latest"; exit 1; }
command -v gobind >/dev/null || { echo "gobind not found, run: go install golang.org/x/mobile/cmd/gobind@latest"; exit 1; }
[ -d "$ANDROID_NDK_HOME" ] || { echo "NDK not found at $ANDROID_NDK_HOME"; exit 1; }

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT/relay"

echo "Building gomobile .aar..."
gomobile bind -v -trimpath -ldflags="-s -w" -target=android -androidapi 23 -o mobile.aar ./androidbind/ 2>&1

echo "Copying .aar to android-app/libs..."
mkdir -p ../android-app/app/libs
cp mobile.aar ../android-app/app/libs/mobile.aar

echo "Building Pion relay for Android..."
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ../android-app/app/src/main/jniLibs/arm64-v8a/librelay.so .
GOOS=linux GOARCH=arm go build -trimpath -ldflags="-s -w" -o ../android-app/app/src/main/jniLibs/armeabi-v7a/librelay.so .
echo "Pion relay built"

echo "Done. .aar size: $(du -h mobile.aar | cut -f1)"

echo ""
echo "Building desktop relay..."
go -C "$ROOT/relay" build -trimpath -ldflags="-s -w" -o relay .

echo "Building headless-vk-creator..."
go -C "$ROOT/headless/vk" build -trimpath -ldflags="-s -w" -o headless-vk-creator .

echo "Building headless-telemost-creator-creator..."
go -C "$ROOT/headless/telemost" build -trimpath -ldflags="-s -w" -o headless-telemost-creator .

echo "Building headless-wbstream-creator..."
go -C "$ROOT/headless/wbstream" build -trimpath -ldflags="-s -w" -o headless-wbstream-creator .

echo "Building headless-dion-creator..."
go -C "$ROOT/headless/dion" build -trimpath -ldflags="-s -w" -o headless-dion-creator .

echo "Done."
ls -lh "$ROOT/relay/relay" "$ROOT/headless/vk/headless-vk-creator" "$ROOT/headless/telemost/headless-telemost-creator" "$ROOT/headless/wbstream/headless-wbstream-creator" "$ROOT/headless/dion/headless-dion-creator"
