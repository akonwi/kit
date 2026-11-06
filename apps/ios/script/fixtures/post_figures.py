"""Posts a message with a Mermaid diagram and figures, a checklist among
them, into one session on the server of an isolated Kit home, for
script/test_figures.sh. It goes through
the session-message-demo plugin, so copy that plugin into the home first:

    cp -R .kit/plugins/session-message-demo "$KIT_HOME/plugins/"
    KIT_HOME=/tmp/kit-ios-dev python3 apps/ios/script/fixtures/post_figures.py <session-id>

The message starts a turn; the session's model doesn't need to answer it.
"""
import json
import os
import sys
import time
import urllib.error
import urllib.request

home = os.environ.get("KIT_HOME") or sys.exit("Set KIT_HOME to an isolated Kit home whose server is running.")
if os.path.realpath(home) == os.path.realpath(os.path.expanduser("~/.kit")):
    sys.exit("Refusing the default Kit home; use an isolated one.")
session = sys.argv[1] if len(sys.argv) > 1 else sys.exit("Pass a session ID on that server.")

with open(os.path.join(home, "run", "server.json")) as file:
    server = json.load(file)
with open(os.path.join(home, "run", "server.token")) as file:
    token = file.read().strip()
headers = {
    "Authorization": f"Bearer {token}",
    "X-Kit-Instance-ID": server["instanceId"],
    "X-Kit-Protocol-Version": str(server["protocolVersion"]),
    "Content-Type": "application/json",
}


def call(method, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(server["url"] + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(request) as response:
            text = response.read()
            return json.loads(text) if text else None
    except urllib.error.HTTPError as error:
        sys.exit(f"{method} {path}: {error.code} {error.read().decode()}")


call("POST", f"/v1/sessions/{session}/reload")
send = None
for _ in range(20):  # The plugin's commands arrive shortly after the reload.
    commands = call("GET", f"/v1/sessions/{session}").get("pluginCommands") or []
    send = next((c for c in commands if c["id"] == "session-message-demo.send"), None)
    if send:
        break
    time.sleep(0.5)
if send is None:
    sys.exit("The session has no session-message-demo.send command; copy the plugin into $KIT_HOME/plugins.")

message = """Figures demo

```mermaid
flowchart LR
  A[Client] --> B{Paired?}
  B -- yes --> C[Open the event stream]
  B -- no --> D[Show the pairing code]
  C --> E[Draw the transcript]
  D --> A
```

```compare title="Session store"
| | **SQLite** | Postgres | DuckDB |
| --- | --- | --- | --- |
| Embedded | yes | no | yes |
| Concurrent writes | no | yes | no |
| SQL | yes | yes | yes |
| Zero setup | yes | no | yes |
```

```steps title="Release"
1. Freeze the tokens
2. **Ship the registry JSON**

   Waiting on the CDN purge before the registry index can update.

3. Write the postmortem
```

```checklist title="Web adoption"
- [x] Port the figure renderer
- [ ] Web adoption branch — two parts

  Lands once both are done.

  - [x] Rebase on main
  - [ ] **Second approval** — in review
- [ ] Update docs/figures.md
```
"""
call("POST", f"/v1/sessions/{session}/plugin-commands",
     {"id": send["id"], "instance": send["instance"], "args": message})
print("Posted the figures message.")
