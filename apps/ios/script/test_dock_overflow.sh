#!/bin/sh
# Shows a plugin confirmation that is longer and wider than the dock, in the
# app on a simulator, and checks that all of it can be read: the whole title,
# code and a table that scroll sideways, and the message's last line. Runs
# against the server of an isolated Kit home that the app is already paired
# with (see test_pairing.sh). The server must have loaded the fixture plugin,
# so copy it into the home before starting the server:
#
#   cp -R apps/ios/script/fixtures/dock-overflow "$KIT_HOME/plugins/"
#   KIT_HOME=/tmp/kit-ios-dev apps/ios/script/test_dock_overflow.sh "Survey the demo project" [simulator-udid]
#
# No model is needed. Screenshots are kept in the result bundle.
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

# Run the plugin's command in the session. The request returns once the
# confirmation is answered, so it runs in the background while the test does.
KIT_DOCK_TITLE="$title" python3 - <<'PY' &
import json, os, sys, time, urllib.request

home = os.environ["KIT_HOME"]
server = json.load(open(os.path.join(home, "run", "server.json")))
token = open(os.path.join(home, "run", "server.token")).read().strip()
headers = {"Authorization": "Bearer " + token, "X-Kit-Instance-ID": server["instanceId"],
           "X-Kit-Protocol-Version": str(server["protocolVersion"]), "Content-Type": "application/json"}

def call(path, body=None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(server["url"] + path, data=data, headers=headers)
    with urllib.request.urlopen(request, timeout=600) as response:
        return json.load(response) if response.length != 0 else None

title = os.environ["KIT_DOCK_TITLE"]
session = next((s for s in call("/v1/sessions")["sessions"] if s.get("name") == title), None)
if session is None:
    sys.exit(f"No session named {title}")
# The session starts its plugins when it's first opened.
command = None
for _ in range(30):
    commands = call("/v1/sessions/" + session["id"]).get("pluginCommands", [])
    command = next((c for c in commands if c["id"] == "dock-overflow.overflow"), None)
    if command is not None:
        break
    time.sleep(1)
if command is None:
    sys.exit("The server hasn't loaded the dock-overflow plugin; copy it into $KIT_HOME/plugins and restart the server.")
call("/v1/sessions/" + session["id"] + "/plugin-commands",
     {"id": command["id"], "instance": command["instance"], "args": ""})
PY
command=$!
trap 'kill "$command" 2>/dev/null || true' EXIT

cd "$app_dir"
TEST_RUNNER_KIT_DOCK_SESSION="$title" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/dock-overflow-$(date +%s).xcresult" \
  -only-testing:KitUITests/DockOverflowUITests test
