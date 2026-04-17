#!/bin/sh
set -e

export PATH="$PATH:/opt/homebrew/bin:$HOME/go/bin"

command -v go >/dev/null || { echo "go not found"; exit 1; }
command -v gomobile >/dev/null || { echo "gomobile not found, run: go install golang.org/x/mobile/cmd/gomobile@latest"; exit 1; }
command -v gobind >/dev/null || { echo "gobind not found, run: go install golang.org/x/mobile/cmd/gobind@latest"; exit 1; }

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT/relay"

echo "Building gomobile .xcframework for iOS..."
rm -rf "$ROOT/ios-proxy-app/Mobile.xcframework"
gomobile bind -v -target=ios -o "$ROOT/ios-proxy-app/Mobile.xcframework" ./pion/ios/ 2>&1

echo "Done. Size:"
du -sh "$ROOT/ios-proxy-app/Mobile.xcframework"
