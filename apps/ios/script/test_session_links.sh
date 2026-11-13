#!/bin/sh
# Opens a session from a mention in another session's transcript, in the app
# on a simulator, against the server of an isolated Kit home that the app is
# already paired with (see test_pairing.sh). The server plays the stand-in
# model in script/fixtures/long_reply_model.py, which answers the message the
# test sends:
#
#   python3 apps/ios/script/fixtures/long_reply_model.py 60932 &
#   OPENAI_API_KEY=stand-in OPENAI_BASE_URL=http://127.0.0.1:60932/v1/ kit server run ...
#   KIT_HOME=/tmp/kit-ios-links apps/ios/script/test_session_links.sh "Fix SSE reconnect" "Mention target" session_… [simulator-udid]
#
# Screenshots of each step are kept in the result bundle.
set -eu

: "${KIT_HOME:?Set KIT_HOME to an isolated Kit home whose server is running}"
case "$KIT_HOME" in
  "$HOME/.kit" | "$HOME/.kit/") echo "Refusing the default Kit home; use an isolated one." >&2; exit 1 ;;
esac
title=${1:?Pass the title of the session to send the mention from}
target=${2:?Pass the title of the session it mentions}
target_id=${3:?Pass the ID of the session it mentions}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
simulator=${4:-$(xcrun simctl list devices booted | sed -n 's/.*(\([0-9A-F-]\{36\}\)) (Booted).*/\1/p' | head -n 1)}
[ -n "$simulator" ] || { echo "Boot a simulator or pass its UDID." >&2; exit 1; }

cd "$app_dir"
TEST_RUNNER_KIT_LINKS_SESSION="$title" TEST_RUNNER_KIT_LINKS_TARGET="$target" TEST_RUNNER_KIT_LINKS_TARGET_ID="$target_id" \
  xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/session-links-$(date +%s).xcresult" \
  -only-testing:KitUITests/SessionLinksUITests test
