set -u

cookies_path() {
    _p="$1"
    case "$_p" in
        telemost) _f="cookies-yandex.json" ;;
        wbstream) _f="cookies-wbstream.json" ;;
        dion) _f="cookies-dion.json" ;;
        bitrix) _f="cookies-bitrix.json" ;;
        *) _f="cookies-$_p.json" ;;
    esac
    _envname=$(printf 'COOKIES_%s' "$(echo "$_p" | tr 'a-z' 'A-Z')")
    eval "_override=\${$_envname:-}"
    if [ -n "$_override" ]; then
        echo "$_override"
    else
        echo "${COOKIES_DIR:-$E2E_ROOT}/$_f"
    fi
}

room_value() {
    _envname=$(printf 'ROOM_%s' "$(echo "$1" | tr 'a-z' 'A-Z')")
    eval "_v=\${$_envname:-}"
    echo "$_v"
}

pf_select() {
    PF_NAME="$1"
    case "$1" in
        telemost)
            PF_CREATOR="$HEADLESS/telemost/headless-telemost-creator"
            PF_JOINER="$HEADLESS/telemost-joiner/headless-telemost-joiner"
            PF_LINK_FLAG="--tm-link"
            PF_CREATOR_ROOM_FLAG="--tm-link"
            PF_READY_RE="\[tm-ws\] Connected"
            PF_CAPS="connect kcp dual kick"
            ;;
        wbstream)
            PF_CREATOR="$HEADLESS/wbstream/headless-wbstream-creator"
            PF_JOINER="$HEADLESS/wbstream-joiner/headless-wbstream-joiner"
            PF_LINK_FLAG="--room"
            PF_CREATOR_ROOM_FLAG="--room"
            PF_READY_RE="pub PC state: connected"
            PF_CAPS="connect dc kcp dual kick"
            ;;
        dion)
            PF_CREATOR="$HEADLESS/dion/headless-dion-creator"
            PF_JOINER="$HEADLESS/dion-joiner/headless-dion-joiner"
            PF_LINK_FLAG="--room"
            PF_CREATOR_ROOM_FLAG="--room"
            PF_READY_RE="\[ice\] connected"
            PF_CAPS="connect kick"
            ;;
        bitrix)
            PF_CREATOR="$HEADLESS/bitrix/headless-bitrix-creator"
            PF_JOINER="$HEADLESS/bitrix-joiner/headless-bitrix-joiner"
            PF_LINK_FLAG="--link"
            PF_CREATOR_ROOM_FLAG="--room"
            PF_READY_RE="pub PC state: connected"
            PF_CAPS="connect dc kcp dual kick"
            ;;
        *)
            die "unknown platform: $1"
            ;;
    esac
    PF_COOKIES=$(cookies_path "$1")
    PF_ROOM=$(room_value "$1")
}

pf_variant_flags() {
    case "$1" in
        dc) echo "--tunnel-mode dc" ;;
        kcp) echo "--reliable" ;;
        dual) echo "--dual-track" ;;
        *) echo "" ;;
    esac
}

pf_has_cap() {
    for _c in $PF_CAPS; do
        [ "$_c" = "$1" ] && return 0
    done
    return 1
}
