#!/bin/sh
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT/android-app"

[ -f "./gradlew" ] || { echo "gradlew not found"; exit 1; }

echo "Cleaning..."
find app/build -name .DS_Store -delete 2>/dev/null || true # dont fail trying to clean last build!
./gradlew clean 2>&1 | tail -3

echo "Building APK..."
./gradlew assembleDebug 2>&1 | tail -5

APK="app/build/outputs/apk/debug/app-debug.apk"
if [ -f "$APK" ]; then
    mkdir -p "$ROOT/prebuilts"
    cp "$APK" "$ROOT/prebuilts/whitelist-bypass.apk"
    echo "APK ready: prebuilts/whitelist-bypass.apk ($(du -h "$ROOT/prebuilts/whitelist-bypass.apk" | cut -f1))"
else
    echo "Build failed, APK not found"
    exit 1
fi
