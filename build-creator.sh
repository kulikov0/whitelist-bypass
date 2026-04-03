#!/bin/sh
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
RELAY_DIR="$ROOT/relay"
CREATOR_DIR="$ROOT/creator-app"

HEADLESS_DIR="$ROOT/headless"

echo "=== Building relay binaries ==="
cd "$RELAY_DIR"

echo "macOS (universal)..."
GOOS=darwin GOARCH=amd64 go build -o relay-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -o relay-darwin-arm64 .
lipo -create -output relay-darwin relay-darwin-amd64 relay-darwin-arm64
rm relay-darwin-amd64 relay-darwin-arm64

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -o relay-windows-x64.exe .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -o relay-windows-ia32.exe .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -o relay-linux-x64 .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -o relay-linux-ia32 .

ls -lh relay-darwin relay-windows-*.exe relay-linux-*

echo ""
echo "=== Building headless-creator ==="
cd "$HEADLESS_DIR"

echo "macOS (universal)..."
GOOS=darwin GOARCH=amd64 go build -o headless-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -o headless-darwin-arm64 .
lipo -create -output headless-darwin headless-darwin-amd64 headless-darwin-arm64
rm headless-darwin-amd64 headless-darwin-arm64

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -o headless-windows-x64.exe .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -o headless-windows-ia32.exe .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -o headless-linux-x64 .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -o headless-linux-ia32 .

ls -lh headless-darwin headless-windows-*.exe headless-linux-*

echo ""
echo "=== Building Electron apps ==="
cd "$CREATOR_DIR"
npm install --quiet 2>&1

# macOS (universal binary already)
echo ""
echo "--- macOS ---"
npx electron-builder --mac || true

# Windows x64
echo ""
echo "--- Windows x64 ---"
cp "$RELAY_DIR/relay-windows-x64.exe" "$RELAY_DIR/relay-bundle.exe"
cp "$HEADLESS_DIR/headless-windows-x64.exe" "$HEADLESS_DIR/headless-bundle.exe"
npx electron-builder --win --x64

# Windows x86
echo ""
echo "--- Windows x86 ---"
cp "$RELAY_DIR/relay-windows-ia32.exe" "$RELAY_DIR/relay-bundle.exe"
cp "$HEADLESS_DIR/headless-windows-ia32.exe" "$HEADLESS_DIR/headless-bundle.exe"
npx electron-builder --win --ia32

# Linux x64
echo ""
echo "--- Linux x64 ---"
cp "$RELAY_DIR/relay-linux-x64" "$RELAY_DIR/relay-bundle"
cp "$HEADLESS_DIR/headless-linux-x64" "$HEADLESS_DIR/headless-bundle"
npx electron-builder --linux --x64

# Copy standalone headless binaries to prebuilts
echo ""
echo "=== Copying headless binaries ==="
mkdir -p "$ROOT/prebuilts"
cp "$HEADLESS_DIR/headless-linux-x64" "$ROOT/prebuilts/headless-creator-linux-x64"
cp "$HEADLESS_DIR/headless-linux-ia32" "$ROOT/prebuilts/headless-creator-linux-ia32"

# Cleanup build artifacts
rm -f "$RELAY_DIR"/relay-darwin "$RELAY_DIR"/relay-windows-*.exe "$RELAY_DIR"/relay-linux-*
rm -f "$RELAY_DIR"/relay-bundle "$RELAY_DIR"/relay-bundle.exe
rm -f "$HEADLESS_DIR"/headless-darwin "$HEADLESS_DIR"/headless-windows-*.exe "$HEADLESS_DIR"/headless-linux-*
rm -f "$HEADLESS_DIR"/headless-bundle "$HEADLESS_DIR"/headless-bundle.exe

echo ""
echo "=== Done ==="
ls -lh "$ROOT/prebuilts/"
