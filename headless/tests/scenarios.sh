set -u

CREATOR_WRAP="${CREATOR_WRAP:-}"
JOINER_WRAP="${JOINER_WRAP:-}"
JOINER_SOCKS_HOST="${JOINER_SOCKS_HOST:-127.0.0.1}"

run_platform() {
    pf_select "$1"
    require_bin "$PF_CREATOR"
    require_bin "$PF_JOINER"
    if [ ! -f "$PF_COOKIES" ]; then
        log "=== $1 SKIP (cookies not found at $PF_COOKIES) ==="
        record SKIP "$1 (cookies missing)"
        return
    fi
    if [ -z "$PF_ROOM" ] && [ "${ALLOW_NEW_ROOMS:-0}" != "1" ]; then
        log "=== $1 SKIP (no ROOM_$(echo "$1" | tr 'a-z' 'A-Z') set; refusing to create a new room. Set it in .env or pass ALLOW_NEW_ROOMS=1) ==="
        record SKIP "$1 (no room)"
        return
    fi
    if ! start_creator; then
        record FAIL "$1 (creator)"
        return
    fi
    for _s in $RUN_SCENARIOS; do
        if ! proc_alive "$CREATOR_PID"; then
            log "=== $1 ABORT: creator died mid-run ==="
            record FAIL "$1 (creator died)"
            break
        fi
        case "$_s" in
            connect) sc_connect ;;
            dc) sc_params dc ;;
            kcp) sc_params kcp ;;
            dual) sc_params dual ;;
            kick) sc_kick ;;
        esac
        kill_joiners
        [ "$ABORT_RUN" = "1" ] && {
            log "=== $1 ABORT: scenario failed (fail-fast); stopping ==="
            break
        }
        sleep 1
    done
    stop_creator
}

start_creator() {
    CREATOR_LOG=$(mktemp -t e2e-creator.XXXXXX)
    _room_args=""
    if [ -n "$PF_ROOM" ]; then
        _room_args="$PF_CREATOR_ROOM_FLAG $PF_ROOM"
        log "  reusing room: $PF_ROOM"
    fi
    $CREATOR_WRAP "$PF_CREATOR" --cookies "$PF_COOKIES" $_room_args --allow-private-dst >"$CREATOR_LOG" 2>&1 &
    CREATOR_PID=$!
    track_pid "$CREATOR_PID"
    if ! JOIN_LINK=$(wait_join_link "$CREATOR_LOG" "$CREATE_TIMEOUT" "$CREATOR_PID"); then
        if proc_alive "$CREATOR_PID"; then
            log "  creator did not print join_link in ${CREATE_TIMEOUT}s"
        else
            log "  creator exited before join_link (fatal)"
        fi
        tail -15 "$CREATOR_LOG" >&2
        return 1
    fi
    log "  join_link=$JOIN_LINK"
    if ! wait_log_re "$CREATOR_LOG" "$PF_READY_RE" "$CREATE_TIMEOUT" "$CREATOR_PID"; then
        if proc_alive "$CREATOR_PID"; then
            log "  creator did not reach ready state ($PF_READY_RE) in ${CREATE_TIMEOUT}s"
        else
            log "  creator exited before ready state (fatal)"
        fi
        tail -15 "$CREATOR_LOG" >&2
        return 1
    fi
    log "  creator up on SFU, room stays for all scenarios"
    sleep 1
    return 0
}

stop_creator() {
    [ -n "${CREATOR_PID:-}" ] && kill "$CREATOR_PID" 2>/dev/null
    CREATOR_PID=""
}

dump_logs() {
    log "  --- creator tail ---"
    tail -20 "$CREATOR_LOG" >&2
    log "  --- joiner tail ---"
    tail -20 "$JOINER_LOG" >&2
}

dump_kick_logs() {
    log "  --- creator kick events ---"
    grep -nE '\[kick\]|kicked previous|active call guest|kick failed|usersAnswered|kicked stale peer|kick_one' "$CREATOR_LOG" 2>/dev/null | tail -20 >&2
    log "  --- joiner A kick-watch ---"
    grep -nE 'subws2|pull config|kick-detect|kicked from|shutting down|chatUserLeave|you_kicked|server kicked' "$_log_a" 2>/dev/null | tail -20 >&2
}

start_joiner() {
    _variant="$1"
    alloc_port
    JOINER_PORT="$ALLOC_PORT"
    JOINER_LOG=$(mktemp -t e2e-joiner.XXXXXX)
    _flags=$(pf_variant_flags "$_variant")
    $JOINER_WRAP "$PF_JOINER" "$PF_LINK_FLAG" "$JOIN_LINK" --socks-host "$JOINER_SOCKS_HOST" --socks-port "$JOINER_PORT" $_flags >"$JOINER_LOG" 2>&1 &
    JOINER_PID=$!
    track_pid "$JOINER_PID"
    track_joiner "$JOINER_PID"
}

joiner_up() {
    wait_socks "$1" "$CONNECT_TIMEOUT" && probe_ready "$1" "$PROBE_TIMEOUT"
}

sc_connect() {
    _name="$PF_NAME/connect"
    log "=== $_name ==="
    start_joiner connect
    if joiner_up "$JOINER_PORT"; then
        record PASS "$_name"
    else
        log "  joiner did not carry traffic"
        dump_logs
        record FAIL "$_name"
    fi
}

sc_params() {
    _variant="$1"
    _name="$PF_NAME/params:$_variant"
    if ! pf_has_cap "$_variant"; then
        log "=== $_name SKIP (unsupported) ==="
        record SKIP "$_name"
        return
    fi
    log "=== $_name ==="
    start_joiner "$_variant"
    if joiner_up "$JOINER_PORT"; then
        record PASS "$_name"
    else
        log "  joiner did not carry traffic"
        dump_logs
        record FAIL "$_name"
    fi
}

sc_kick() {
    _name="$PF_NAME/kick"
    if ! pf_has_cap kick; then
        log "=== $_name SKIP ==="
        record SKIP "$_name"
        return
    fi
    log "=== $_name ==="
    _kv=connect
    pf_has_cap dc && _kv=dc
    start_joiner "$_kv"
    _port_a="$JOINER_PORT"
    _log_a="$JOINER_LOG"
    if ! joiner_up "$_port_a"; then
        log "  joiner A never carried traffic; tail:"
        tail -15 "$_log_a" >&2
        dump_kick_logs
        record FAIL "$_name (A up)"
        return
    fi
    log "  joiner A up on $_port_a, joining second joiner"
    start_joiner "$_kv"
    _port_b="$JOINER_PORT"
    _log_b="$JOINER_LOG"
    if ! joiner_up "$_port_b"; then
        log "  joiner B never carried traffic; tail:"
        tail -15 "$_log_b" >&2
        dump_kick_logs
        record FAIL "$_name (B up)"
        return
    fi
    log "  joiner B up on $_port_b, expecting A to be kicked"
    if probe_dies "$_port_a" "$KICK_TIMEOUT" && probe_once "$_port_b"; then
        record PASS "$_name"
    else
        log "  expected A kicked while B holds the tunnel"
        dump_kick_logs
        record FAIL "$_name"
    fi
}
