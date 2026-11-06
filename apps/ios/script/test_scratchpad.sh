#!/bin/sh
# Edits one session's shared scratchpad in the app on a simulator, against
# the server of an isolated Kit home that the app is already paired with (see
# test_pairing.sh). The test also plays a second client through the server's
# API, reading that home's run/server.json and server.token, so it can change
# the shared copy under the app's draft. It replaces the session's pad.
#
#   KIT_HOME=/tmp/kit-ios-dev apps/ios/script/test_scratchpad.sh "Files demo" session_… [simulator-udid]
#
# No model is needed. Screenshots of each step are kept in the result bundle.
set -eu

: "${KIT_HOME:?Set KIT_HOME to an isolated Kit home whose server is running}"
case "$KIT_HOME" in
  "$HOME/.kit" | "$HOME/.kit/") echo "Refusing the default Kit home; use an isolated one." >&2; exit 1 ;;
esac
title=${1:?Pass the title of a session on that server}
session=${2:?Pass the session ID}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
simulator=${3:-$(xcrun simctl list devices booted | sed -n 's/.*(\([0-9A-F-]\{36\}\)) (Booted).*/\1/p' | head -n 1)}
[ -n "$simulator" ] || { echo "Boot a simulator or pass its UDID." >&2; exit 1; }

cd "$app_dir"
TEST_RUNNER_KIT_SCRATCHPAD_SESSION="$title" TEST_RUNNER_KIT_SCRATCHPAD_SESSION_ID="$session" \
TEST_RUNNER_KIT_SCRATCHPAD_HOME="$(CDPATH= cd -- "$KIT_HOME" && pwd)" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/scratchpad-$(date +%s).xcresult" \
  -only-testing:KitUITests/ScratchpadUITests test
