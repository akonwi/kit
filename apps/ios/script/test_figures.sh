#!/bin/sh
# Opens a Mermaid diagram from one session's transcript on its own, pinches
# it, and closes it, in the app on a simulator, against the server of an
# isolated Kit home that the app is already paired with (see
# test_pairing.sh). Post the diagram into the session first with
# script/fixtures/post_figures.py:
#
#   KIT_HOME=/tmp/kit-ios-dev apps/ios/script/test_figures.sh "Figures demo" [simulator-udid]
#
# Screenshots of each step are kept in the result bundle.
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
TEST_RUNNER_KIT_FIGURES_SESSION="$title" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/figures-$(date +%s).xcresult" \
  -only-testing:KitUITests/FiguresUITests test
