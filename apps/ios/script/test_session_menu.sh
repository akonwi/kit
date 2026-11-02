#!/bin/sh
# Opens the session menu in one session in the app on a simulator, against
# the server of an isolated Kit home that the app is already paired with (see
# test_pairing.sh). It renames the session and back, and forks it and deletes
# the fork. With KIT_MENU_MODEL naming a model that offers thinking levels, as
# the menu writes it, it also switches to that model, changes the thinking
# level, and switches both back. No turn starts:
#
#   KIT_HOME=/tmp/kit-ios-dev [KIT_MENU_MODEL="GPT-5.5"] \
#     apps/ios/script/test_session_menu.sh "Survey the demo project" [simulator-udid]
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
TEST_RUNNER_KIT_MENU_SESSION="$title" TEST_RUNNER_KIT_MENU_MODEL="${KIT_MENU_MODEL:-}" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/session-menu-$(date +%s).xcresult" \
  -only-testing:KitUITests/SessionMenuUITests test
