#!/bin/sh
set -eu

DIR="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$DIR/../../.." && pwd)"
E2E="$REPO/headless/tests"
CTX="$DIR/.context"
IMAGE=wlb-e2e-stand

COMPONENTS="\
telemost/headless-telemost-creator \
wbstream/headless-wbstream-creator \
dion/headless-dion-creator \
bitrix/headless-bitrix-creator \
telemost-joiner/headless-telemost-joiner \
wbstream-joiner/headless-wbstream-joiner \
dion-joiner/headless-dion-joiner \
bitrix-joiner/headless-bitrix-joiner"

usage() {
    cat >&2 <<EOF
Usage: stand.sh build | run [platform...] [scenario...] | sh

Dockerized live e2e: each of creator and joiner runs in its own network
namespace (distinct IP), so same-host WebRTC ICE does not collide. Traffic is
proved by echoing a nonce through the joiner's SOCKS5 to a sink on the creator's
loopback - no external network, no rate limits.

  build   cross-build linux binaries, stage cookies, docker build
  run     run the netns harness in a privileged container
  sh      drop into a shell in the container for debugging

Platforms: telemost wbstream dion bitrix   (VK has no headless SOCKS joiner)
Scenarios: connect dc kcp dual kick

Cookies are taken from cookies-<platform>.json at the repo root at build time.
Rebuild after refreshing cookies. Examples:
  stand.sh build
  stand.sh run
  stand.sh run bitrix dc kick
EOF
    exit 1
}

build_context() {
    arch=$(docker version --format '{{.Server.Arch}}' 2>/dev/null || echo arm64)
    rm -rf "$CTX"
    mkdir -p "$CTX/work/headless/tests/stand"
    for spec in $COMPONENTS; do
        d="${spec%%/*}"
        b="${spec##*/}"
        mkdir -p "$CTX/work/headless/$d"
        echo "building $b (linux/$arch)..."
        GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
            go -C "$REPO/headless/$d" build -trimpath -ldflags="-s -w" -o "$CTX/work/headless/$d/$b" .
    done
    cp "$E2E/lib.sh" "$E2E/platforms.sh" "$E2E/scenarios.sh" "$CTX/work/headless/tests/"
    [ -f "$E2E/.env" ] && cp "$E2E/.env" "$CTX/work/headless/tests/"
    cp "$DIR/netns.sh" "$DIR/netns-run.sh" "$CTX/work/headless/tests/stand/"
    cp "$DIR/Dockerfile" "$CTX/Dockerfile"
}

case "${1:-}" in
    build)
        build_context
        docker build -t "$IMAGE" "$CTX"
        echo "built $IMAGE"
        ;;
    run)
        shift
        cookie_mounts=""
        for c in vk yandex wbstream dion bitrix; do
            f="$REPO/cookies-$c.json"
            [ -f "$f" ] && cookie_mounts="$cookie_mounts -v $f:/cookies/cookies-$c.json"
        done
        docker run --rm --cap-add=NET_ADMIN --cap-add=SYS_ADMIN \
            -e FAIL_FAST="${FAIL_FAST:-1}" \
            -e COOKIES_DIR=/cookies \
            $cookie_mounts \
            "$IMAGE" /work/headless/tests/stand/netns-run.sh "$@"
        ;;
    sh)
        docker run --rm -it --cap-add=NET_ADMIN --cap-add=SYS_ADMIN \
            "$IMAGE" /bin/bash
        ;;
    *)
        usage
        ;;
esac
