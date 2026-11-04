#!/bin/sh
# A comment whose code changed, in the app on a simulator, against the
# server of an isolated Kit home that the app is already paired with (see
# test_pairing.sh). The session's folder must be a git repository with
# uncommitted changes that add lines to FILE, and no comments. Through the
# server's API it comments on FILE's first added line, then changes that
# line; the app shows the comment as changed, moves it to another line, and
# deletes it. FILE is restored afterwards:
#
#   KIT_HOME=/tmp/kit-ios-dev apps/ios/script/test_changed_comment.sh \
#     "Survey the demo project" client.go [simulator-udid]
#
# Screenshots of each step are kept in the result bundle.
set -eu

: "${KIT_HOME:?Set KIT_HOME to an isolated Kit home whose server is running}"
case "$KIT_HOME" in
  "$HOME/.kit" | "$HOME/.kit/") echo "Refusing the default Kit home; use an isolated one." >&2; exit 1 ;;
esac
title=${1:?Pass the title of a session on that server}
file=${2:?Pass a file the uncommitted changes add lines to}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
simulator=${3:-$(xcrun simctl list devices booted | sed -n 's/.*(\([0-9A-F-]\{36\}\)) (Booted).*/\1/p' | head -n 1)}
[ -n "$simulator" ] || { echo "Boot a simulator or pass its UDID." >&2; exit 1; }

# Comment on the file's first added line, then change it. Prints the file's
# path on this machine and the line.
setup=$(KIT_TITLE="$title" KIT_FILE="$file" python3 - <<'PY'
import json, os, urllib.request
home = os.environ["KIT_HOME"]
run = json.load(open(os.path.join(home, "run", "server.json")))
token = open(os.path.join(home, "run", "server.token")).read().strip()
def call(method, path, body=None):
    request = urllib.request.Request(run["url"] + path, method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Authorization": "Bearer " + token, "X-Kit-Instance-ID": run["instanceId"],
                 "X-Kit-Protocol-Version": str(run["protocolVersion"]), "Content-Type": "application/json"})
    with urllib.request.urlopen(request) as response:
        text = response.read()
        return json.loads(text) if text else None
sessions = call("GET", "/v1/sessions")
sessions = sessions.get("sessions", sessions) if isinstance(sessions, dict) else sessions
session = next(s for s in sessions if (s.get("name") or s.get("title") or "").startswith(os.environ["KIT_TITLE"]))
sid = session.get("id") or session.get("sessionId")
workspace = call("GET", "/v1/sessions/" + sid + "/workspace")
page = call("POST", "/v1/sessions/" + sid + "/diff/working-tree", {"workspaceId": workspace["workspaceId"]})
observation = page["observation"]
summary = next(f for f in page["files"] if f["path"] == os.environ["KIT_FILE"])
diff = call("POST", "/v1/sessions/" + sid + "/diff/files/read", {"targetId": observation["target"]["id"],
    "targetRevision": observation["revision"], "path": summary["path"]})
line = next(l["newLine"] for h in diff["hunks"] for l in h["lines"] if l["kind"] == "addition" and l["content"].strip())
call("POST", "/v1/sessions/" + sid + "/annotations", {"anchor": {"kind": "working_tree_diff", "workingTreeDiff": {
    "targetId": observation["target"]["id"], "targetRevision": observation["revision"], "path": summary["path"],
    "fileRevision": summary["fileRevision"], "side": "new", "startLine": line, "endLine": line}},
    "body": "Is this line still right?"})
print(os.path.join(workspace["cwd"], summary["path"]), line)
PY
)
path=${setup% *}
line=${setup##* }
cp "$path" "$path.kit-test"
trap 'mv "$path.kit-test" "$path"' EXIT
sed -i '' "${line}s/\$/ \/\/ changed under the comment/" "$path"

cd "$app_dir"
TEST_RUNNER_KIT_COMMENTS_SESSION="$title" \
  xcodebuild -project Kit.xcodeproj -scheme Kit \
  -destination "platform=iOS Simulator,id=$simulator" -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -resultBundlePath ".build/changed-comment-$(date +%s).xcresult" \
  -only-testing:KitUITests/CommentsUITests/testChangedComment test
