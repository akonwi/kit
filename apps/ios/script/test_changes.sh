#!/bin/sh
# Opens Changes in one session in the app on a simulator, against the server
# of an isolated Kit home that the app is already paired with (see
# test_pairing.sh). The session's folder must be a git repository with
# uncommitted changes to FILE and a recent commit whose subject starts with
# COMMIT and that touches greet.go. It reads the diff, folds a file, wraps
# lines, compares the commit, and goes back to uncommitted changes:
#
#   KIT_HOME=/tmp/kit-ios-dev apps/ios/script/test_changes.sh \
#     "Survey the demo project" client.go "fix(greet)" [simulator-udid]
#
# Screenshots of each step are kept in the result bundle.
set -eu

: "${KIT_HOME:?Set KIT_HOME to an isolated Kit home whose server is running}"
case "$KIT_HOME" in
  "$HOME/.kit" | "$HOME/.kit/") echo "Refusing the default Kit home; use an isolated one." >&2; exit 1 ;;
esac
title=${1:?Pass the title of a session on that server}
file=${2:?Pass a file the uncommitted changes touch}
commit=${3:?Pass the start of a recent commit subject}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
simulator=${4:-$(xcrun simctl list devices booted | sed -n 's/.*(\([0-9A-F-]\{36\}\)) (Booted).*/\1/p' | head -n 1)}
[ -n "$simulator" ] || { echo "Boot a simulator or pass its UDID." >&2; exit 1; }

cd "$app_dir"
TEST_RUNNER_KIT_CHANGES_SESSION="$title" TEST_RUNNER_KIT_CHANGES_FILE="$file" TEST_RUNNER_KIT_CHANGES_COMMIT="$commit" \
  xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/changes-$(date +%s).xcresult" \
  -only-testing:KitUITests/ChangesUITests test
