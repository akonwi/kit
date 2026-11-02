#!/bin/sh
# Opens sessions in the app on a simulator, one after another, against the
# server of an isolated Kit home that the app is already paired with (see
# test_pairing.sh). Pass the titles of sessions on that server:
#
#   KIT_HOME=/tmp/kit-ios-dev apps/ios/script/test_sessions.sh "Fix SSE reconnect|Release notes" [simulator-udid]
#
# Screenshots of the list and each transcript are kept in the result bundle.
set -eu

: "${KIT_HOME:?Set KIT_HOME to an isolated Kit home whose server is running}"
case "$KIT_HOME" in
  "$HOME/.kit" | "$HOME/.kit/") echo "Refusing the default Kit home; use an isolated one." >&2; exit 1 ;;
esac
titles=${1:?Pass session titles separated by |}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
simulator=${2:-$(xcrun simctl list devices booted | sed -n 's/.*(\([0-9A-F-]\{36\}\)) (Booted).*/\1/p' | head -n 1)}
[ -n "$simulator" ] || { echo "Boot a simulator or pass its UDID." >&2; exit 1; }

cd "$app_dir"
TEST_RUNNER_KIT_SESSION_TITLES="$titles" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/sessions-$(date +%s).xcresult" \
  -only-testing:KitUITests/SessionsUITests test
