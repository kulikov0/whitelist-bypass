#!/bin/sh
set -e

export PATH="$PATH:/opt/homebrew/bin:$HOME/go/bin"

command -v go >/dev/null || { echo "go not found"; exit 1; }
command -v gomobile >/dev/null || { echo "gomobile not found, run: go install golang.org/x/mobile/cmd/gomobile@latest"; exit 1; }
command -v gobind >/dev/null || { echo "gobind not found, run: go install golang.org/x/mobile/cmd/gobind@latest"; exit 1; }
command -v xcodebuild >/dev/null || { echo "xcodebuild not found, install Xcode"; exit 1; }

ROOT="$(cd "$(dirname "$0")" && pwd)"

VARIANT="${1:-}"
case "$VARIANT" in
    proxy)
        APP_DIR="$ROOT/ios-proxy-app"
        PROJECT="whitelist-bypass-proxy.xcodeproj"
        SCHEME="whitelist-bypass-proxy"
        CONFIGURATION="Debug"
        APP_NAME="whitelist-bypass-proxy.app"
        IPA_NAME="whitelist-bypass-proxy.ipa"
        ;;
    vpn)
        APP_DIR="$ROOT/ios-vpn-app"
        PROJECT="whitelist-bypass-vpn.xcodeproj"
        SCHEME="WhitelistBypassVPN"
        CONFIGURATION="Release"
        APP_NAME="WhitelistBypassVPN.app"
        IPA_NAME="whitelist-bypass-vpn.ipa"
        ;;
    *)
        echo "usage: $0 proxy|vpn"
        exit 1
        ;;
esac

XCFRAMEWORK="$APP_DIR/Mobile.xcframework"
BUILD_DIR="$APP_DIR/build"
APP_PATH="$BUILD_DIR/$CONFIGURATION-iphoneos/$APP_NAME"
IPA_PATH="$ROOT/prebuilts/$IPA_NAME"

echo "==> [$VARIANT] building Mobile.xcframework via gomobile..."
cd "$ROOT/relay"
rm -rf "$XCFRAMEWORK"
gomobile bind -v -trimpath -ldflags="-s -w" -target=ios -o "$XCFRAMEWORK" ./pion/ios/ 2>&1
echo "xcframework size: $(du -sh "$XCFRAMEWORK" | cut -f1)"

if [ -f "$APP_DIR/strip-signing.sh" ]; then
    echo "==> [$VARIANT] stripping developer team from the Xcode project..."
    sh "$APP_DIR/strip-signing.sh"
fi

echo "==> [$VARIANT] building app via xcodebuild, unsigned..."
cd "$APP_DIR"
rm -rf "$BUILD_DIR"
xcodebuild \
    -project "$PROJECT" \
    -scheme "$SCHEME" \
    -configuration "$CONFIGURATION" \
    -sdk iphoneos \
    -destination 'generic/platform=iOS' \
    SYMROOT="$BUILD_DIR" \
    CODE_SIGNING_ALLOWED=NO \
    CODE_SIGNING_REQUIRED=NO \
    CODE_SIGN_IDENTITY="" \
    build

[ -d "$APP_PATH" ] || { echo "build failed: $APP_PATH not found"; exit 1; }

echo "==> [$VARIANT] packaging unsigned IPA..."
mkdir -p "$ROOT/prebuilts"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT
mkdir -p "$TEMP_DIR/Payload"
cp -R "$APP_PATH" "$TEMP_DIR/Payload/"

PAYLOAD_APP="$TEMP_DIR/Payload/$APP_NAME"
find "$PAYLOAD_APP" -type f \( -name "*.dylib" -o -perm -u+x \) -print0 2>/dev/null | while IFS= read -r -d '' bin; do
    codesign --remove-signature "$bin" 2>/dev/null || true
done
find "$PAYLOAD_APP" -name "_CodeSignature" -type d -exec rm -rf {} + 2>/dev/null || true
find "$PAYLOAD_APP" -name "embedded.mobileprovision" -delete 2>/dev/null || true

rm -f "$IPA_PATH"
cd "$TEMP_DIR"
zip -qr "$IPA_PATH" Payload/ -x "*.DS_Store"

echo "==> [$VARIANT] created: $IPA_PATH, $(du -h "$IPA_PATH" | cut -f1)"
echo "sign with your own identity in Xcode, or sideload; the Network Extension capability must be enabled on your bundle id for vpn."
