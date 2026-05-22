#!/bin/sh
set -eu

: "${VK_TOKEN:?VK_TOKEN is required}"
: "${VK_GROUP_ID:?VK_GROUP_ID is required}"

BINS_DIR="${BINS_DIR:-/opt/wlb/bin}"
SESSIONS_DIR="${SESSIONS_DIR:-/data/sessions}"
RESOURCES="${RESOURCES:-default}"

VK_COOKIES_DEFAULT="/data/cookies-vk.json"
TM_COOKIES_DEFAULT="/data/cookies-telemost.json"
WB_COOKIES_DEFAULT="/data/cookies-wbstream.json"
DION_COOKIES_DEFAULT="/data/cookies-dion.json"
VK_COOKIES="${VK_COOKIES:-}"
TM_COOKIES="${TM_COOKIES:-}"
WB_COOKIES="${WB_COOKIES:-}"
DION_COOKIES="${DION_COOKIES:-}"
[ -z "$VK_COOKIES" ] && [ -f "$VK_COOKIES_DEFAULT" ] && VK_COOKIES="$VK_COOKIES_DEFAULT"
[ -z "$TM_COOKIES" ] && [ -f "$TM_COOKIES_DEFAULT" ] && TM_COOKIES="$TM_COOKIES_DEFAULT"
[ -z "$WB_COOKIES" ] && [ -f "$WB_COOKIES_DEFAULT" ] && WB_COOKIES="$WB_COOKIES_DEFAULT"
[ -z "$DION_COOKIES" ] && [ -f "$DION_COOKIES_DEFAULT" ] && DION_COOKIES="$DION_COOKIES_DEFAULT"

mkdir -p "$SESSIONS_DIR"

set -- \
    --token "$VK_TOKEN" \
    --group-id "$VK_GROUP_ID" \
    --bins-dir "$BINS_DIR" \
    --sessions-dir "$SESSIONS_DIR" \
    --resources "$RESOURCES"

[ -n "${VK_USER_IDS:-}" ] && set -- "$@" --user-id "$VK_USER_IDS"
[ -n "$VK_COOKIES" ] && set -- "$@" --vk-cookies "$VK_COOKIES"
[ -n "$TM_COOKIES" ] && set -- "$@" --tm-cookies "$TM_COOKIES"
[ -n "$WB_COOKIES" ] && set -- "$@" --wb-cookies "$WB_COOKIES"
[ -n "$DION_COOKIES" ] && set -- "$@" --dion-cookies "$DION_COOKIES"

exec /usr/local/bin/headless-vk-bot "$@"
