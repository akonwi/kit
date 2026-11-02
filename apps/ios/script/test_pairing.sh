#!/bin/sh
# Pairs the app in a simulator with the server of an isolated Kit home, end
# to end. The server must already be running for that home; this script
# never starts one and refuses the default home:
#
#   KIT_HOME=/tmp/kit-ios-dev kit server run &
#   KIT_HOME=/tmp/kit-ios-dev apps/ios/script/test_pairing.sh [simulator-udid]
#
# It reinstalls the app, so the simulator starts with no paired servers.
set -eu

: "${KIT_HOME:?Set KIT_HOME to an isolated Kit home whose server is running}"
case "$KIT_HOME" in
  "$HOME/.kit" | "$HOME/.kit/") echo "Refusing the default Kit home; use an isolated one." >&2; exit 1 ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
simulator=${1:-$(xcrun simctl list devices booted | sed -n 's/.*(\([0-9A-F-]\{36\}\)) (Booted).*/\1/p' | head -n 1)}
[ -n "$simulator" ] || { echo "Boot a simulator or pass its UDID." >&2; exit 1; }

link=$(kit server pair | sed -n 's/^ *\(http[^ ]*\/pair#[a-z2-7]*\)$/\1/p' | tail -n 1)
[ -n "$link" ] || { echo "kit server pair printed no link." >&2; exit 1; }

xcrun simctl uninstall "$simulator" com.akonwi.kit.dev 2>/dev/null || true
cd "$app_dir"
TEST_RUNNER_KIT_PAIRING_LINK="$link" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/pairing-$(date +%s).xcresult" \
  -only-testing:KitUITests/PairingUITests test
