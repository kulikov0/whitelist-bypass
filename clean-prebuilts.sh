#!/bin/sh
set -e

# Removes electron-builder scratch files from prebuilts/ so only the
# final shippable artifacts remain (per-arch installers / AppImages and
# the standalone headless CLIs).

ROOT="$(cd "$(dirname "$0")" && pwd)"
PREBUILTS="$ROOT/prebuilts"

rm -rf "$PREBUILTS"/win-unpacked \
       "$PREBUILTS"/win-ia32-unpacked \
       "$PREBUILTS"/win-arm64-unpacked \
       "$PREBUILTS"/linux-unpacked \
       "$PREBUILTS"/linux-arm64-unpacked \
       "$PREBUILTS"/mac \
       "$PREBUILTS"/mac-arm64 \
       "$PREBUILTS"/.icon-set

rm -f "$PREBUILTS"/*.blockmap \
      "$PREBUILTS"/builder-debug.yml \
      "$PREBUILTS"/builder-effective-config.yaml \
      "$PREBUILTS"/latest*.yml

# electron-builder emits an arch-less duplicate alongside the per-arch
# files when multiple archs build in one invocation. Drop everything
# that isn't tagged with x64 / ia32 / arm64.
for f in "$PREBUILTS"/"WhitelistBypass "*-*.exe \
         "$PREBUILTS"/"WhitelistBypass "*-*.AppImage \
         "$PREBUILTS"/"WhitelistBypass "*-*.dmg \
         "$PREBUILTS"/"WhitelistBypass "*-*.zip; do
    [ -e "$f" ] || continue
    case "$f" in
        *-x64.exe|*-ia32.exe|*-arm64.exe) ;;
        *-x86_64.AppImage|*-arm64.AppImage) ;;
        *-x64.dmg|*-arm64.dmg|*-universal.dmg) ;;
        *-x64.zip|*-arm64.zip|*-universal.zip) ;;
        *) rm -f "$f" ;;
    esac
done
