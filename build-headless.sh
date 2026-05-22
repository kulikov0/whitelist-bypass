#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"

echo "Building headless-vk-creator..."
go -C "$ROOT/headless/vk" build -trimpath -ldflags="-s -w" -o headless-vk-creator .

echo "Building headless-telemost-creator..."
go -C "$ROOT/headless/telemost" build -trimpath -ldflags="-s -w" -o headless-telemost-creator .

echo "Building headless-wbstream-creator..."
go -C "$ROOT/headless/wbstream" build -trimpath -ldflags="-s -w" -o headless-wbstream-creator .

echo "Building headless-dion-creator..."
go -C "$ROOT/headless/dion" build -trimpath -ldflags="-s -w" -o headless-dion-creator .

echo "Building headless-dion-joiner..."
go -C "$ROOT/headless/dion-joiner" build -trimpath -ldflags="-s -w" -o headless-dion-joiner .

echo "Building headless-wbstream-joiner..."
go -C "$ROOT/headless/wbstream-joiner" build -trimpath -ldflags="-s -w" -o headless-wbstream-joiner .

echo "Building headless-telemost-joiner..."
go -C "$ROOT/headless/telemost-joiner" build -trimpath -ldflags="-s -w" -o headless-telemost-joiner .

echo "Building headless-vk-bot..."
go -C "$ROOT/headless/vk-bot" build -trimpath -ldflags="-s -w" -o headless-vk-bot .

echo "Done."
ls -lh "$ROOT/headless/vk/headless-vk-creator" "$ROOT/headless/telemost/headless-telemost-creator" "$ROOT/headless/wbstream/headless-wbstream-creator" "$ROOT/headless/dion/headless-dion-creator" "$ROOT/headless/dion-joiner/headless-dion-joiner" "$ROOT/headless/wbstream-joiner/headless-wbstream-joiner" "$ROOT/headless/telemost-joiner/headless-telemost-joiner" "$ROOT/headless/vk-bot/headless-vk-bot"
