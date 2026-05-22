#!/bin/sh
set -e

export PATH="$PATH:/opt/homebrew/bin:$HOME/go/bin"

command -v go >/dev/null || { echo "go not found"; exit 1; }
command -v gomobile >/dev/null || { echo "gomobile not found, run: go install golang.org/x/mobile/cmd/gomobile@latest"; exit 1; }
command -v gobind >/dev/null || { echo "gobind not found, run: go install golang.org/x/mobile/cmd/gobind@latest"; exit 1; }
command -v xcodebuild >/dev/null || { echo "xcodebuild not found, install Xcode command line tools"; exit 1; }

ROOT="$(cd "$(dirname "$0")" && pwd)"
APP_BUILD_DIR="$ROOT/ios-proxy-app/build/Debug-iphoneos"
APP_PATH="$APP_BUILD_DIR/whitelist-bypass-proxy.app"
IPA_PATH="$ROOT/prebuilts/whitelist-bypass-proxy.ipa"

echo "Building gomobile .xcframework for iOS..."
cd "$ROOT/relay"
rm -rf "$ROOT/ios-proxy-app/Mobile.xcframework"
gomobile bind -v -trimpath -ldflags="-s -w" -target=ios -o "$ROOT/ios-proxy-app/Mobile.xcframework" ./pion/ios/ 2>&1

echo "xcframework size:"
du -sh "$ROOT/ios-proxy-app/Mobile.xcframework"

echo "Building .app via xcodebuild..."
cd "$ROOT/ios-proxy-app"
rm -rf "$APP_BUILD_DIR"
xcodebuild \
    -project whitelist-bypass-proxy.xcodeproj \
    -scheme whitelist-bypass-proxy \
    -configuration Debug \
    -sdk iphoneos \
    -destination 'generic/platform=iOS' \
    CONFIGURATION_BUILD_DIR="$APP_BUILD_DIR" \
    CODE_SIGNING_ALLOWED=NO \
    CODE_SIGNING_REQUIRED=NO \
    CODE_SIGN_IDENTITY="" \
    build

echo "Packaging unsigned IPA..."
mkdir -p "$ROOT/prebuilts"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT
mkdir -p "$TEMP_DIR/Payload"
cp -r "$APP_PATH" "$TEMP_DIR/Payload/"

codesign --remove-signature "$TEMP_DIR/Payload/whitelist-bypass-proxy.app/whitelist-bypass-proxy" 2>/dev/null || true
find "$TEMP_DIR/Payload/whitelist-bypass-proxy.app/Frameworks" -mindepth 2 -maxdepth 2 -type f ! -name "Info.plist" -exec codesign --remove-signature {} \; 2>/dev/null || true

rm -f "$IPA_PATH"
cd "$TEMP_DIR"
zip -r "$IPA_PATH" Payload/ -x "*.DS_Store"

echo "Created: $IPA_PATH"
echo "Size: $(du -h "$IPA_PATH" | cut -f1)"
