set -u

PASS_N=0
FAIL_N=0
SKIP_N=0
RESULTS=""
FAIL_FAST="${FAIL_FAST:-1}"
ABORT_RUN=0
CHILD_PIDS=""
JOINER_PIDS=""
JOINER_FIFO=""
RESOLVER_PID=""
SINK_PID=""
SINK_PORT=""
SINK_FILE=""
SINK_WRAP="${SINK_WRAP:-}"
SINK_BIND="${SINK_BIND:-}"
SINK_TARGET="${SINK_TARGET:-}"
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

host_ip() {
    if command -v ip >/dev/null 2>&1; then
        ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p' | head -1
    elif command -v ipconfig >/dev/null 2>&1; then
        _iface=$(route -n get default 2>/dev/null | sed -n 's/.*interface: *//p' | head -1)
        [ -n "$_iface" ] && ipconfig getifaddr "$_iface" 2>/dev/null
    fi
}

start_sink() {
    [ -n "$SINK_BIND" ] || SINK_BIND=$(host_ip)
    [ -n "$SINK_BIND" ] || die "cannot detect a non-loopback address for the sink, set SINK_BIND and SINK_TARGET"
    [ -n "$SINK_TARGET" ] || SINK_TARGET="$SINK_BIND"
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
    if [ -n "$RESOLVER_PID" ]; then
        kill "$RESOLVER_PID" 2>/dev/null
        RESOLVER_PID=""
    fi
    exec 3>&-
    rm -f "$JOINER_FIFO"
    JOINER_FIFO=""
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

resolve_host() {
    if command -v getent >/dev/null 2>&1; then
        getent ahostsv4 "$1" 2>/dev/null | awk '{print $1; exit}'
    elif command -v dig >/dev/null 2>&1; then
        dig +short A "$1" 2>/dev/null | grep -m1 '^[0-9]'
    else
        ping -c1 -W1 "$1" 2>/dev/null | sed -n '1s/.*(\([0-9.]*\)).*/\1/p'
    fi
}

answer_resolves() {
    _lf="$1"
    _done=0
    while :; do
        _total=$(grep -c '^RESOLVE:' "$_lf" 2>/dev/null || true)
        [ -n "$_total" ] || _total=0
        while [ "$_done" -lt "$_total" ]; do
            _done=$((_done + 1))
            _host=$(grep '^RESOLVE:' "$_lf" | sed -n "${_done}p" | sed 's/^RESOLVE://')
            _ip=$(resolve_host "$_host")
            [ -n "$_ip" ] || _ip="0.0.0.0"
            log "  resolve $_host -> $_ip"
            printf '%s\n' "$_ip" >&3
        done
        sleep 1
    done
}

open_captcha() {
    _lf="$1"
    _end=$(($(now) + CONNECT_TIMEOUT))
    while [ "$(now)" -lt "$_end" ]; do
        _url=$(grep -m1 -o 'CAPTCHA:http://[^[:space:]]*' "$_lf" 2>/dev/null | sed 's/^CAPTCHA://')
        if [ -n "$_url" ]; then
            log "  captcha required, opening $_url"
            if command -v open >/dev/null 2>&1; then
                open "$_url"
            elif command -v xdg-open >/dev/null 2>&1; then
                xdg-open "$_url"
            else
                log "  no browser opener on this host, solve it manually at $_url"
            fi
            return 0
        fi
        nc -z "$PROBE_HOST" "$JOINER_PORT" 2>/dev/null && return 0
        proc_alive "$JOINER_PID" || return 1
        sleep 1
    done
    return 0
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
