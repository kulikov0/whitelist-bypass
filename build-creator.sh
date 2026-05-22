#!/bin/sh
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
RELAY_DIR="$ROOT/relay"
CREATOR_DIR="$ROOT/creator-app"
HEADLESS_DIR="$ROOT/headless"
HEADLESS_VK_DIR="$HEADLESS_DIR/vk"
HEADLESS_TM_DIR="$HEADLESS_DIR/telemost"
HEADLESS_WB_DIR="$HEADLESS_DIR/wbstream"
HEADLESS_DION_DIR="$HEADLESS_DIR/dion"

echo "=== Building relay binaries ==="
cd "$RELAY_DIR"

echo "macOS (universal)..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o relay-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o relay-darwin-arm64 .
lipo -create -output relay-darwin relay-darwin-amd64 relay-darwin-arm64
rm relay-darwin-amd64 relay-darwin-arm64

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o relay-windows-x64.exe .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -trimpath -ldflags="-s -w" -o relay-windows-ia32.exe .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o relay-linux-x64 .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o relay-linux-ia32 .

ls -lh relay-darwin relay-windows-*.exe relay-linux-*

echo ""
echo "=== Building headless-vk-creator ==="
cd "$HEADLESS_VK_DIR"

echo "macOS (universal)..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-vk-darwin-amd64" .
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-vk-darwin-arm64" .
lipo -create -output "$HEADLESS_DIR/headless-vk-darwin" "$HEADLESS_DIR/headless-vk-darwin-amd64" "$HEADLESS_DIR/headless-vk-darwin-arm64"
rm "$HEADLESS_DIR/headless-vk-darwin-amd64" "$HEADLESS_DIR/headless-vk-darwin-arm64"

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-vk-windows-x64.exe" .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-vk-windows-ia32.exe" .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-vk-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-vk-linux-ia32" .

echo ""
echo "=== Building headless-telemost-creator ==="
cd "$HEADLESS_TM_DIR"

echo "macOS (universal)..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-telemost-darwin-amd64" .
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-telemost-darwin-arm64" .
lipo -create -output "$HEADLESS_DIR/headless-telemost-darwin" "$HEADLESS_DIR/headless-telemost-darwin-amd64" "$HEADLESS_DIR/headless-telemost-darwin-arm64"
rm "$HEADLESS_DIR/headless-telemost-darwin-amd64" "$HEADLESS_DIR/headless-telemost-darwin-arm64"

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-telemost-windows-x64.exe" .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-telemost-windows-ia32.exe" .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-telemost-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-telemost-linux-ia32" .

echo ""
echo "=== Building headless-wbstream-creator ==="
cd "$HEADLESS_WB_DIR"

echo "macOS (universal)..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-wbstream-darwin-amd64" .
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-wbstream-darwin-arm64" .
lipo -create -output "$HEADLESS_DIR/headless-wbstream-darwin" "$HEADLESS_DIR/headless-wbstream-darwin-amd64" "$HEADLESS_DIR/headless-wbstream-darwin-arm64"
rm "$HEADLESS_DIR/headless-wbstream-darwin-amd64" "$HEADLESS_DIR/headless-wbstream-darwin-arm64"

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-wbstream-windows-x64.exe" .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-wbstream-windows-ia32.exe" .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-wbstream-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-wbstream-linux-ia32" .

echo ""
echo "=== Building headless-dion-creator ==="
cd "$HEADLESS_DION_DIR"

echo "macOS (universal)..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-darwin-amd64" .
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-darwin-arm64" .
lipo -create -output "$HEADLESS_DIR/headless-dion-darwin" "$HEADLESS_DIR/headless-dion-darwin-amd64" "$HEADLESS_DIR/headless-dion-darwin-arm64"
rm "$HEADLESS_DIR/headless-dion-darwin-amd64" "$HEADLESS_DIR/headless-dion-darwin-arm64"

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-windows-x64.exe" .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-windows-ia32.exe" .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-linux-ia32" .

ls -lh "$HEADLESS_DIR"/headless-vk-darwin "$HEADLESS_DIR"/headless-telemost-darwin "$HEADLESS_DIR"/headless-wbstream-darwin "$HEADLESS_DIR"/headless-dion-darwin

echo ""
echo "=== Building Electron apps ==="
cd "$CREATOR_DIR"
npm install --quiet 2>&1
npm run build 2>&1

# macOS (universal binary already)
echo ""
echo "--- macOS ---"
npx electron-builder --mac || true

# Windows x64
echo ""
echo "--- Windows x64 ---"
cp "$RELAY_DIR/relay-windows-x64.exe" "$RELAY_DIR/relay-bundle.exe"
cp "$HEADLESS_DIR/headless-vk-windows-x64.exe" "$HEADLESS_DIR/headless-vk-bundle.exe"
cp "$HEADLESS_DIR/headless-telemost-windows-x64.exe" "$HEADLESS_DIR/headless-telemost-bundle.exe"
cp "$HEADLESS_DIR/headless-wbstream-windows-x64.exe" "$HEADLESS_DIR/headless-wbstream-bundle.exe"
cp "$HEADLESS_DIR/headless-dion-windows-x64.exe" "$HEADLESS_DIR/headless-dion-bundle.exe"
npx electron-builder --win --x64

# Windows x86
echo ""
echo "--- Windows x86 ---"
cp "$RELAY_DIR/relay-windows-ia32.exe" "$RELAY_DIR/relay-bundle.exe"
cp "$HEADLESS_DIR/headless-vk-windows-ia32.exe" "$HEADLESS_DIR/headless-vk-bundle.exe"
cp "$HEADLESS_DIR/headless-telemost-windows-ia32.exe" "$HEADLESS_DIR/headless-telemost-bundle.exe"
cp "$HEADLESS_DIR/headless-wbstream-windows-ia32.exe" "$HEADLESS_DIR/headless-wbstream-bundle.exe"
cp "$HEADLESS_DIR/headless-dion-windows-ia32.exe" "$HEADLESS_DIR/headless-dion-bundle.exe"
npx electron-builder --win --ia32

# Linux x64
echo ""
echo "--- Linux x64 ---"
cp "$RELAY_DIR/relay-linux-x64" "$RELAY_DIR/relay-bundle"
cp "$HEADLESS_DIR/headless-vk-linux-x64" "$HEADLESS_DIR/headless-vk-bundle"
cp "$HEADLESS_DIR/headless-telemost-linux-x64" "$HEADLESS_DIR/headless-telemost-bundle"
cp "$HEADLESS_DIR/headless-wbstream-linux-x64" "$HEADLESS_DIR/headless-wbstream-bundle"
cp "$HEADLESS_DIR/headless-dion-linux-x64" "$HEADLESS_DIR/headless-dion-bundle"
npx electron-builder --linux --x64

# Copy standalone headless binaries to prebuilts
echo ""
echo "=== Copying headless binaries ==="
mkdir -p "$ROOT/prebuilts"
cp "$HEADLESS_DIR/headless-vk-linux-x64" "$ROOT/prebuilts/headless-vk-creator-linux-x64"
cp "$HEADLESS_DIR/headless-vk-linux-ia32" "$ROOT/prebuilts/headless-vk-creator-linux-ia32"
cp "$HEADLESS_DIR/headless-telemost-linux-x64" "$ROOT/prebuilts/headless-telemost-creator-linux-x64"
cp "$HEADLESS_DIR/headless-telemost-linux-ia32" "$ROOT/prebuilts/headless-telemost-creator-linux-ia32"
cp "$HEADLESS_DIR/headless-wbstream-linux-x64" "$ROOT/prebuilts/headless-wbstream-creator-linux-x64"
cp "$HEADLESS_DIR/headless-wbstream-linux-ia32" "$ROOT/prebuilts/headless-wbstream-creator-linux-ia32"
cp "$HEADLESS_DIR/headless-dion-linux-x64" "$ROOT/prebuilts/headless-dion-creator-linux-x64"
cp "$HEADLESS_DIR/headless-dion-linux-ia32" "$ROOT/prebuilts/headless-dion-creator-linux-ia32"

# Cleanup build artifacts
rm -f "$RELAY_DIR"/relay-darwin "$RELAY_DIR"/relay-windows-*.exe "$RELAY_DIR"/relay-linux-*
rm -f "$RELAY_DIR"/relay-bundle "$RELAY_DIR"/relay-bundle.exe
rm -f "$HEADLESS_DIR"/headless-vk-darwin "$HEADLESS_DIR"/headless-vk-windows-*.exe "$HEADLESS_DIR"/headless-vk-linux-*
rm -f "$HEADLESS_DIR"/headless-vk-bundle "$HEADLESS_DIR"/headless-vk-bundle.exe
rm -f "$HEADLESS_DIR"/headless-telemost-darwin "$HEADLESS_DIR"/headless-telemost-windows-*.exe "$HEADLESS_DIR"/headless-telemost-linux-*
rm -f "$HEADLESS_DIR"/headless-telemost-bundle "$HEADLESS_DIR"/headless-telemost-bundle.exe
rm -f "$HEADLESS_DIR"/headless-wbstream-darwin "$HEADLESS_DIR"/headless-wbstream-windows-*.exe "$HEADLESS_DIR"/headless-wbstream-linux-*
rm -f "$HEADLESS_DIR"/headless-wbstream-bundle "$HEADLESS_DIR"/headless-wbstream-bundle.exe
rm -f "$HEADLESS_DIR"/headless-dion-darwin "$HEADLESS_DIR"/headless-dion-windows-*.exe "$HEADLESS_DIR"/headless-dion-linux-*
rm -f "$HEADLESS_DIR"/headless-dion-bundle "$HEADLESS_DIR"/headless-dion-bundle.exe

"$ROOT/clean-prebuilts.sh"

echo ""
echo "=== Done ==="
ls -lh "$ROOT/prebuilts/"
