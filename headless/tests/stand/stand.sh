#!/bin/sh
set -eu

DIR="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$DIR/../../.." && pwd)"
E2E="$REPO/headless/tests"
CTX="$DIR/.context"
IMAGE=wlb-e2e-stand

COMPONENTS="\
vk/headless-vk-creator \
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

  build   cross-build linux binaries, docker build
  run     run the netns harness in a privileged container
  sh      drop into a shell in the container for debugging

See headless/tests/README.md.
EOF
    exit 1
}

build_context() {
    arch=$(docker version --format '{{.Server.Arch}}' 2>/dev/null | tr -d '[:space:]')
    [ -n "$arch" ] || arch=arm64
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
    mkdir -p "$CTX/work/relay"
    echo "building relay (linux/$arch)..."
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
        go -C "$REPO/relay" build -trimpath -ldflags="-s -w" -o "$CTX/work/relay/relay" .
    cp "$E2E/lib.sh" "$E2E/platforms.sh" "$E2E/scenarios.sh" "$CTX/work/headless/tests/"
    cp "$DIR/netns.sh" "$DIR/netns-run.sh" "$CTX/work/headless/tests/stand/"
    cp "$DIR/Dockerfile" "$CTX/Dockerfile"
}

bridge_captcha() {
    _container="$1"
    _pidfile="$2"
    _seen=""
    while :; do
        sleep 2
        for _url in $(docker logs "$_container" 2>&1 | grep -o 'http://127\.0\.0\.1:[0-9][0-9]*/' | sort -u); do
            _port=$(echo "$_url" | sed 's|.*:\([0-9][0-9]*\)/|\1|')
            case " $_seen " in
                *" $_port "*) continue ;;
            esac
            _seen="$_seen $_port"
            echo "captcha at $_url, bridging into the joiner namespace"
            ncat -l 127.0.0.1 "$_port" --keep-open \
                --sh-exec "docker exec -i $_container ip netns exec joiner ncat 127.0.0.1 $_port" &
            echo $! >>"$_pidfile"
            open "$_url" 2>/dev/null || echo "open it yourself: $_url"
        done
    done
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
        env_mount=""
        [ -f "$E2E/.env" ] && env_mount="-v $E2E/.env:/work/headless/tests/.env"
        container="$IMAGE-run"
        pidfile=$(mktemp -t wlb-captcha.XXXXXX)
        docker rm -f "$container" >/dev/null 2>&1 || true
        bridge_captcha "$container" "$pidfile" &
        watcher=$!
        status=0
        docker run --rm --name "$container" --cap-add=NET_ADMIN --cap-add=SYS_ADMIN \
            -e FAIL_FAST="${FAIL_FAST:-1}" \
            -e COOKIES_DIR=/cookies \
            $cookie_mounts $env_mount \
            "$IMAGE" /work/headless/tests/stand/netns-run.sh "$@" || status=$?
        if kill "$watcher" 2>/dev/null; then
            wait "$watcher" 2>/dev/null || true
        fi
        if [ -s "$pidfile" ]; then
            kill $(cat "$pidfile") 2>/dev/null || true
        fi
        rm -f "$pidfile"
        exit "$status"
        ;;
    sh)
        docker run --rm -it --cap-add=NET_ADMIN --cap-add=SYS_ADMIN \
            "$IMAGE" /bin/bash
        ;;
    *)
        usage
        ;;
esac
