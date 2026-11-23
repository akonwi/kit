#!/bin/sh
# Reads the detail of each kind of model question in the dock, in one session
# in the app on a simulator, against the server of an isolated Kit home that
# the app is already paired with (see test_pairing.sh). The server plays the
# stand-in model in script/fixtures/questions_model.py instead of a real one,
# and the session must use an OpenAI model:
#
#   python3 apps/ios/script/fixtures/questions_model.py 60951 &
#   OPENAI_API_KEY=stand-in OPENAI_BASE_URL=http://127.0.0.1:60951/v1/ kit server run ...
#   KIT_HOME=/tmp/kit-ios-dev apps/ios/script/test_dock_detail.sh "Questions demo" [simulator-udid]
#
# It starts the turn that asks the questions. Screenshots are kept in the
# result bundle.
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

KIT_DOCK_TITLE="$title" python3 - <<'PY'
import json, os, sys, urllib.request

home = os.environ["KIT_HOME"]
server = json.load(open(os.path.join(home, "run", "server.json")))
token = open(os.path.join(home, "run", "server.token")).read().strip()
headers = {"Authorization": "Bearer " + token, "X-Kit-Instance-ID": server["instanceId"],
           "X-Kit-Protocol-Version": str(server["protocolVersion"]), "Content-Type": "application/json"}

def call(path, body=None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(server["url"] + path, data=data, headers=headers)
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response) if response.length != 0 else None

title = os.environ["KIT_DOCK_TITLE"]
session = next((s for s in call("/v1/sessions")["sessions"] if s.get("name") == title), None)
if session is None:
    sys.exit(f"No session named {title}")
call("/v1/sessions/" + session["id"] + "/turns/prompts", {"text": "Ask me the release questions."})
PY

cd "$app_dir"
TEST_RUNNER_KIT_DOCK_DETAIL_SESSION="$title" xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/dock-detail-$(date +%s).xcresult" \
  -only-testing:KitUITests/DockDetailUITests test
