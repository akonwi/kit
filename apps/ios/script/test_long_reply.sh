#!/bin/sh
# Sends a message whose reply is several screens long, then one whose reply
# fits, in the app on a simulator, against the server of an isolated Kit home
# that the app is already paired with (see test_pairing.sh). The server plays
# the stand-in model in script/fixtures/long_reply_model.py instead of a real
# one:
#
#   python3 apps/ios/script/fixtures/long_reply_model.py 60932 &
#   OPENAI_API_KEY=stand-in OPENAI_BASE_URL=http://127.0.0.1:60932/v1/ kit server run ...
#   KIT_HOME=/tmp/kit-ios-long apps/ios/script/test_long_reply.sh "Fix SSE reconnect" [simulator-udid]
#
# Screenshots of each reply are kept in the result bundle.
set -eu

: "${KIT_HOME:?Set KIT_HOME to an isolated Kit home whose server is running}"
case "$KIT_HOME" in
  "$HOME/.kit" | "$HOME/.kit/") echo "Refusing the default Kit home; use an isolated one." >&2; exit 1 ;;
esac
title=${1:?Pass the title of a session on that server}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
simulator=${2:-$(xcrun simctl list devices booted | sed -n 's/.*(\([0-9A-F-]\{36\}\)) (Booted).*/\1/p' | head -n 1)}
[ -n "$simulator" ] || { echo "Boot a simulator or pass its UDID." >&2; exit 1; }

cd "$app_dir"
TEST_RUNNER_KIT_LONG_REPLY_SESSION="$title" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/long-reply-$(date +%s).xcresult" \
  -only-testing:KitUITests/LongReplyUITests test
