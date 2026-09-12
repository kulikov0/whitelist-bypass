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
HEADLESS_BITRIX_DIR="$HEADLESS_DIR/bitrix"
PREBUILTS="$ROOT/prebuilts"

verify_artifact() {
    artifact_name="$1"
    if [ ! -f "$PREBUILTS/$artifact_name" ]; then
        echo "packaging failed, $artifact_name is missing from prebuilts"
        exit 1
    fi
    echo "packaged $artifact_name $(du -h "$PREBUILTS/$artifact_name" | cut -f1)"
}

stage_darwin_binaries() {
    darwin_arch="$1"
    cp "$RELAY_DIR/relay-darwin-$darwin_arch" "$RELAY_DIR/relay-darwin"
    cp "$HEADLESS_DIR/headless-vk-darwin-$darwin_arch" "$HEADLESS_DIR/headless-vk-darwin"
    cp "$HEADLESS_DIR/headless-telemost-darwin-$darwin_arch" "$HEADLESS_DIR/headless-telemost-darwin"
    cp "$HEADLESS_DIR/headless-wbstream-darwin-$darwin_arch" "$HEADLESS_DIR/headless-wbstream-darwin"
    cp "$HEADLESS_DIR/headless-dion-darwin-$darwin_arch" "$HEADLESS_DIR/headless-dion-darwin"
    cp "$HEADLESS_DIR/headless-bitrix-darwin-$darwin_arch" "$HEADLESS_DIR/headless-bitrix-darwin"
}

echo "=== Building relay binaries ==="
cd "$RELAY_DIR"

echo "macOS x64..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o relay-darwin-amd64 .
echo "macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o relay-darwin-arm64 .

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o relay-windows-x64.exe .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -trimpath -ldflags="-s -w" -o relay-windows-ia32.exe .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o relay-linux-x64 .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o relay-linux-ia32 .

ls -lh relay-darwin-* relay-windows-*.exe relay-linux-*

echo ""
echo "=== Building headless-vk-creator ==="
cd "$HEADLESS_VK_DIR"

echo "macOS x64..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-vk-darwin-amd64" .
echo "macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-vk-darwin-arm64" .

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

echo "macOS x64..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-telemost-darwin-amd64" .
echo "macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-telemost-darwin-arm64" .

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

echo "macOS x64..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-wbstream-darwin-amd64" .
echo "macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-wbstream-darwin-arm64" .

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

echo "macOS x64..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-darwin-amd64" .
echo "macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-darwin-arm64" .

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-windows-x64.exe" .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-windows-ia32.exe" .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-dion-linux-ia32" .

echo ""
echo "=== Building headless-bitrix-creator ==="
cd "$HEADLESS_BITRIX_DIR"

echo "macOS x64..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-bitrix-darwin-amd64" .
echo "macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-bitrix-darwin-arm64" .

echo "Windows x64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-bitrix-windows-x64.exe" .
echo "Windows x86..."
GOOS=windows GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-bitrix-windows-ia32.exe" .

echo "Linux x64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-bitrix-linux-x64" .
echo "Linux x86..."
GOOS=linux GOARCH=386 go build -trimpath -ldflags="-s -w" -o "$HEADLESS_DIR/headless-bitrix-linux-ia32" .

ls -lh "$HEADLESS_DIR"/headless-vk-darwin-* "$HEADLESS_DIR"/headless-telemost-darwin-* "$HEADLESS_DIR"/headless-wbstream-darwin-* "$HEADLESS_DIR"/headless-dion-darwin-* "$HEADLESS_DIR"/headless-bitrix-darwin-*

echo ""
echo "=== Building Electron apps ==="
cd "$CREATOR_DIR"
npm install --quiet 2>&1
npm run build 2>&1

PRODUCT_NAME="$(node -p "require('$CREATOR_DIR/package.json').build.productName")"
APP_VERSION="$(node -p "require('$CREATOR_DIR/package.json').version")"

# macOS x64
echo ""
echo "--- macOS x64 ---"
stage_darwin_binaries amd64
rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-x64.dmg"
npx electron-builder --mac --x64
verify_artifact "$PRODUCT_NAME-$APP_VERSION-x64.dmg"

# macOS arm64
echo ""
echo "--- macOS arm64 ---"
stage_darwin_binaries arm64
rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-arm64.dmg"
npx electron-builder --mac --arm64
verify_artifact "$PRODUCT_NAME-$APP_VERSION-arm64.dmg"

# Windows x64
echo ""
echo "--- Windows x64 ---"
cp "$RELAY_DIR/relay-windows-x64.exe" "$RELAY_DIR/relay-bundle.exe"
cp "$HEADLESS_DIR/headless-vk-windows-x64.exe" "$HEADLESS_DIR/headless-vk-bundle.exe"
cp "$HEADLESS_DIR/headless-telemost-windows-x64.exe" "$HEADLESS_DIR/headless-telemost-bundle.exe"
cp "$HEADLESS_DIR/headless-wbstream-windows-x64.exe" "$HEADLESS_DIR/headless-wbstream-bundle.exe"
cp "$HEADLESS_DIR/headless-dion-windows-x64.exe" "$HEADLESS_DIR/headless-dion-bundle.exe"
cp "$HEADLESS_DIR/headless-bitrix-windows-x64.exe" "$HEADLESS_DIR/headless-bitrix-bundle.exe"
rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-x64.exe"
npx electron-builder --win --x64
verify_artifact "$PRODUCT_NAME-$APP_VERSION-x64.exe"

# Windows x86
echo ""
echo "--- Windows x86 ---"
cp "$RELAY_DIR/relay-windows-ia32.exe" "$RELAY_DIR/relay-bundle.exe"
cp "$HEADLESS_DIR/headless-vk-windows-ia32.exe" "$HEADLESS_DIR/headless-vk-bundle.exe"
cp "$HEADLESS_DIR/headless-telemost-windows-ia32.exe" "$HEADLESS_DIR/headless-telemost-bundle.exe"
cp "$HEADLESS_DIR/headless-wbstream-windows-ia32.exe" "$HEADLESS_DIR/headless-wbstream-bundle.exe"
cp "$HEADLESS_DIR/headless-dion-windows-ia32.exe" "$HEADLESS_DIR/headless-dion-bundle.exe"
cp "$HEADLESS_DIR/headless-bitrix-windows-ia32.exe" "$HEADLESS_DIR/headless-bitrix-bundle.exe"
rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-ia32.exe"
npx electron-builder --win --ia32
verify_artifact "$PRODUCT_NAME-$APP_VERSION-ia32.exe"

# Linux x64
echo ""
echo "--- Linux x64 ---"
cp "$RELAY_DIR/relay-linux-x64" "$RELAY_DIR/relay-bundle"
cp "$HEADLESS_DIR/headless-vk-linux-x64" "$HEADLESS_DIR/headless-vk-bundle"
cp "$HEADLESS_DIR/headless-telemost-linux-x64" "$HEADLESS_DIR/headless-telemost-bundle"
cp "$HEADLESS_DIR/headless-wbstream-linux-x64" "$HEADLESS_DIR/headless-wbstream-bundle"
cp "$HEADLESS_DIR/headless-dion-linux-x64" "$HEADLESS_DIR/headless-dion-bundle"
cp "$HEADLESS_DIR/headless-bitrix-linux-x64" "$HEADLESS_DIR/headless-bitrix-bundle"
rm -f "$PREBUILTS/$PRODUCT_NAME-$APP_VERSION-x86_64.AppImage"
npx electron-builder --linux --x64
verify_artifact "$PRODUCT_NAME-$APP_VERSION-x86_64.AppImage"

# Cleanup build artifacts
rm -f "$RELAY_DIR"/relay-darwin* "$RELAY_DIR"/relay-windows-*.exe "$RELAY_DIR"/relay-linux-*
rm -f "$RELAY_DIR"/relay-bundle "$RELAY_DIR"/relay-bundle.exe
rm -f "$HEADLESS_DIR"/headless-vk-darwin* "$HEADLESS_DIR"/headless-vk-windows-*.exe "$HEADLESS_DIR"/headless-vk-linux-*
rm -f "$HEADLESS_DIR"/headless-vk-bundle "$HEADLESS_DIR"/headless-vk-bundle.exe
rm -f "$HEADLESS_DIR"/headless-telemost-darwin* "$HEADLESS_DIR"/headless-telemost-windows-*.exe "$HEADLESS_DIR"/headless-telemost-linux-*
rm -f "$HEADLESS_DIR"/headless-telemost-bundle "$HEADLESS_DIR"/headless-telemost-bundle.exe
rm -f "$HEADLESS_DIR"/headless-wbstream-darwin* "$HEADLESS_DIR"/headless-wbstream-windows-*.exe "$HEADLESS_DIR"/headless-wbstream-linux-*
rm -f "$HEADLESS_DIR"/headless-wbstream-bundle "$HEADLESS_DIR"/headless-wbstream-bundle.exe
rm -f "$HEADLESS_DIR"/headless-dion-darwin* "$HEADLESS_DIR"/headless-dion-windows-*.exe "$HEADLESS_DIR"/headless-dion-linux-*
rm -f "$HEADLESS_DIR"/headless-dion-bundle "$HEADLESS_DIR"/headless-dion-bundle.exe
rm -f "$HEADLESS_DIR"/headless-bitrix-darwin* "$HEADLESS_DIR"/headless-bitrix-windows-*.exe "$HEADLESS_DIR"/headless-bitrix-linux-*
rm -f "$HEADLESS_DIR"/headless-bitrix-bundle "$HEADLESS_DIR"/headless-bitrix-bundle.exe

"$ROOT/clean-prebuilts.sh"

echo ""
echo "=== Done ==="
ls -lh "$ROOT/prebuilts/"
