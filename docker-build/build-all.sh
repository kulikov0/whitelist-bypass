#!/usr/bin/env bash
set -euo pipefail

ROOT=/workspace
REPO="${ROOT}/whitelist-bypass"

export HOME=/root
export ANDROID_HOME=/root/Library/Android/sdk
export ANDROID_SDK_ROOT=${ANDROID_HOME}
export ANDROID_NDK_HOME=${ANDROID_HOME}/ndk/29.0.14206865
export GRADLE_USER_HOME=/opt/gradle
export PATH=/usr/local/go/bin:/root/go/bin:${ANDROID_HOME}/cmdline-tools/latest/bin:${ANDROID_HOME}/platform-tools:${PATH}

cd "${REPO}/relay"
gomobile init -v

cd "${REPO}"
echo ""
echo "=== build-go.sh ==="
./build-go.sh

cd "${REPO}"
echo ""
echo "=== build-app.sh ==="
./build-app.sh

cd "${REPO}"
echo ""
echo "=== build-creator.sh ==="
./build-creator.sh

cd "${REPO}"
echo ""
echo "=== ALL BUILDS DONE ==="
ls -lh "${REPO}/prebuilts" || true