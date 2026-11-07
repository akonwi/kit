#!/bin/sh
# Switches a session's transcript between Main and its subagents, and
# dismisses one, in the app on a simulator, against the server of an
# isolated Kit home that the app is already paired with (see
# test_pairing.sh). The server plays a scripted model instead of a real
# one; set it up with script/fixtures/make_subagents_demo.py:
#
#   KIT_HOME=/tmp/kit-ios-agents apps/ios/script/test_subagents.sh "Fix SSE reconnect" [simulator-udid]
#
# The dismiss test removes designer, so set the session up again before
# running the script a second time. Screenshots of each step are kept in the result
# bundle.
set -eu

: "${KIT_HOME:?Set KIT_HOME to an isolated Kit home whose server is running}"
case "$KIT_HOME" in
  "$HOME/.kit" | "$HOME/.kit/") echo "Refusing the default Kit home; use an isolated one." >&2; exit 1 ;;
esac
title=${1:?Pass the title of the session whose main agent started the subagents}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
simulator=${2:-$(xcrun simctl list devices booted | sed -n 's/.*(\([0-9A-F-]\{36\}\)) (Booted).*/\1/p' | head -n 1)}
[ -n "$simulator" ] || { echo "Boot a simulator or pass its UDID." >&2; exit 1; }

cd "$app_dir"
TEST_RUNNER_KIT_SUBAGENTS_SESSION="$title" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/subagents-$(date +%s).xcresult" \
  -only-testing:KitUITests/SubagentsUITests/test1SwitchBetweenAgents \
  -only-testing:KitUITests/SubagentsUITests/test2DismissAnAgent test
