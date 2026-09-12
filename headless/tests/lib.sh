set -u

PASS_N=0
FAIL_N=0
SKIP_N=0
RESULTS=""
FAIL_FAST="${FAIL_FAST:-1}"
ABORT_RUN=0
CHILD_PIDS=""
JOINER_PIDS=""
SINK_PID=""
SINK_PORT=""
SINK_FILE=""
SINK_WRAP="${SINK_WRAP:-}"
SINK_BIND="${SINK_BIND:-127.0.0.1}"
SINK_TARGET="${SINK_TARGET:-127.0.0.1}"
PROBE_HOST="${PROBE_HOST:-127.0.0.1}"
PORT_CURSOR="${PORT_BASE:-21080}"

PROBE_CAP=""
if command -v timeout >/dev/null 2>&1; then
    PROBE_CAP="timeout 5"
elif command -v gtimeout >/dev/null 2>&1; then
    PROBE_CAP="gtimeout 5"
fi
ALLOC_PORT=""
PROBE_SEQ=0

log() { printf '%s\n' "$*" >&2; }

die() {
    log "FATAL: $*"
    exit 2
}

require_bin() {
    [ -x "$1" ] || die "missing binary $1 (run ./build-headless.sh)"
}

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "missing command $1"
}

track_pid() { CHILD_PIDS="$CHILD_PIDS $1"; }

proc_alive() { kill -0 "$1" 2>/dev/null; }

track_joiner() { JOINER_PIDS="$JOINER_PIDS $1"; }

alloc_port() {
    while nc -z 127.0.0.1 "$PORT_CURSOR" 2>/dev/null; do
        PORT_CURSOR=$((PORT_CURSOR + 1))
    done
    ALLOC_PORT="$PORT_CURSOR"
    PORT_CURSOR=$((PORT_CURSOR + 1))
}

start_sink() {
    alloc_port
    SINK_PORT="$ALLOC_PORT"
    SINK_FILE=$(mktemp -t e2e-sink.XXXXXX)
    $SINK_WRAP ncat -l -k "$SINK_BIND" "$SINK_PORT" >"$SINK_FILE" 2>/dev/null &
    SINK_PID=$!
    track_pid "$SINK_PID"
    sleep 1
}

stop_sink() {
    [ -n "$SINK_PID" ] && kill "$SINK_PID" 2>/dev/null
    SINK_PID=""
}

kill_joiners() {
    for _p in $JOINER_PIDS; do kill "$_p" 2>/dev/null; done
    for _p in $JOINER_PIDS; do wait "$_p" 2>/dev/null; done
    JOINER_PIDS=""
}

cleanup() {
    stop_sink
    for _p in $CHILD_PIDS; do kill "$_p" 2>/dev/null; done
    wait 2>/dev/null
}

now() { date +%s; }

wait_join_link() {
    _lf="$1"
    _to="$2"
    _pid="${3:-}"
    _end=$(($(now) + _to))
    while [ "$(now)" -lt "$_end" ]; do
        _l=$(grep -m1 "join_link:" "$_lf" 2>/dev/null | sed 's/.*join_link:[[:space:]]*//')
        if [ -n "$_l" ]; then
            echo "$_l"
            return 0
        fi
        if [ -n "$_pid" ] && ! proc_alive "$_pid"; then
            return 1
        fi
        sleep 1
    done
    return 1
}

wait_log_re() {
    _lf="$1"
    _re="$2"
    _to="$3"
    _pid="${4:-}"
    _end=$(($(now) + _to))
    while [ "$(now)" -lt "$_end" ]; do
        grep -qE "$_re" "$_lf" 2>/dev/null && return 0
        if [ -n "$_pid" ] && ! proc_alive "$_pid"; then
            return 1
        fi
        sleep 1
    done
    return 1
}

wait_socks() {
    _port="$1"
    _to="$2"
    _end=$(($(now) + _to))
    while [ "$(now)" -lt "$_end" ]; do
        nc -z "$PROBE_HOST" "$_port" 2>/dev/null && return 0
        sleep 1
    done
    return 1
}

probe_once() {
    _sp="$1"
    _nonce="ping-$_sp-$PROBE_SEQ"
    PROBE_SEQ=$((PROBE_SEQ + 1))
    printf '%s\n' "$_nonce" | $PROBE_CAP ncat --send-only -w 4 --proxy "$PROBE_HOST:$_sp" --proxy-type socks5 "$SINK_TARGET" "$SINK_PORT" >/dev/null 2>&1
    sleep 1
    grep -q "$_nonce" "$SINK_FILE" 2>/dev/null
}

probe_ready() {
    _sp="$1"
    _to="$2"
    _end=$(($(now) + _to))
    while [ "$(now)" -lt "$_end" ]; do
        probe_once "$_sp" && return 0
    done
    return 1
}

probe_dies() {
    _sp="$1"
    _to="$2"
    _end=$(($(now) + _to))
    while [ "$(now)" -lt "$_end" ]; do
        probe_once "$_sp" || return 0
    done
    return 1
}

record() {
    case "$1" in
        PASS) PASS_N=$((PASS_N + 1)) ;;
        FAIL)
            FAIL_N=$((FAIL_N + 1))
            [ "$FAIL_FAST" = "1" ] && ABORT_RUN=1
            ;;
        SKIP) SKIP_N=$((SKIP_N + 1)) ;;
    esac
    RESULTS="$RESULTS
$1  $2"
}

summary() {
    log ""
    log "=== summary ==="
    printf '%s\n' "$RESULTS" | sed '/^[[:space:]]*$/d' >&2
    log ""
    log "passed=$PASS_N failed=$FAIL_N skipped=$SKIP_N"
    [ "$FAIL_N" -eq 0 ]
}
