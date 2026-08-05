# External plugins

Kit external plugins are independent processes that communicate with Kit using
JSON-RPC 2.0 over stdio. A plugin can be written in any language that can read
and write newline-delimited JSON.

The [protocol specification](../plugin-protocol/v1.md) is the complete v1
contract. The adjacent
[manifest](../plugin-protocol/manifest.schema.json) and
[protocol](../plugin-protocol/protocol.schema.json) JSON Schemas are normative.

## Installation layout

Kit discovers one `plugin.json` manifest in each immediate child directory:

```text
~/.kit/plugins/
  speech/
    plugin.json
    plugin.py

project/.kit/plugins/
  project-policy/
    plugin.json
    policy.rb
```

User plugins load first, followed by project plugins. Installations are sorted
lexically within each scope. A child directory may be a symlink to a standalone
plugin repository whose `plugin.json` is at its root.

Legacy direct `~/.kit/plugins/*.ts` and `.kit/plugins/*.ts` files are not
loaded. Kit does not install dependencies or bundle plugin source.

## Manifest

`plugin.json` declares a stable plugin id and one stdio process:

```json
{
  "$schema": "https://raw.githubusercontent.com/akonwi/kit/main/app/docs/plugin-protocol/manifest.schema.json",
  "manifestVersion": 1,
  "id": "speech",
  "name": "Speech",
  "transport": {
    "type": "stdio",
    "command": "python3",
    "args": ["-u", "plugin.py"]
  }
}
```

Kit launches without a shell and uses the installation directory as the
process cwd. Bare commands resolve through `PATH`; commands containing a path
resolve relative to the installation directory. Arguments are passed literally.

Plugin ids are lowercase kebab-case, contain at most 32 characters, and cannot
use `kit` or `kit-*`. If two manifests use the same id, the first manifest wins
and Kit reports both paths in a persistent error toast.

## Minimal Python plugin

This dependency-free example registers canonical command `hello.greet`, which
is presented as `/greet`. It reserves stdout for protocol frames and writes
diagnostics to stderr:

```python
#!/usr/bin/env python3
import json
import sys

def send(message):
    print(json.dumps(message), flush=True)

for line in sys.stdin:
    if not line.strip():
        continue
    message = json.loads(line)
    method = message.get("method")

    if method == "initialize":
        send({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {"protocolVersion": 1},
        })
        send({
            "jsonrpc": "2.0",
            "id": "register-greet",
            "method": "kit/commands/register",
            "params": {
                "id": "greet",
                "description": "Show a greeting",
            },
        })
    elif method == "kit/commands/execute":
        send({
            "jsonrpc": "2.0",
            "method": "kit/ui/toast",
            "params": {
                "title": "Hello from Python",
                "variant": "info",
            },
        })
        send({"jsonrpc": "2.0", "id": message["id"], "result": None})
    elif method == "shutdown":
        send({"jsonrpc": "2.0", "id": message["id"], "result": None})
        break
```

Its manifest uses id `hello` and points to the script:

```json
{
  "manifestVersion": 1,
  "id": "hello",
  "transport": {
    "type": "stdio",
    "command": "python3",
    "args": ["-u", "plugin.py"]
  }
}
```

Plugins must continue reading while handlers are pending. Requests are
full-duplex: Kit may invoke a plugin command while that plugin is waiting for a
nested Kit UI request. Responses may arrive out of order.

## Initialization context and events

Kit's first request is `initialize`. It contains protocol version 1, the active
project cwd and nullable Git context, and the active session id and nullable
name. The plugin must return `{ "protocolVersion": 1 }` within ten seconds
before registering contributions.

Every ready plugin receives public event notifications without subscribing:

- `kit/events/project.changed`
- `kit/events/git.changed`
- `kit/events/session.changed`
- `kit/events/agent.turn.started`
- `kit/events/agent.turn.completed`

Unknown notifications may be ignored. Completed-turn events contain only
ordered user/assistant text content; internal model, tool, usage, image, and
persistence data is omitted.

## Contributions and ids

Plugins can register commands, tools, a tool-call interceptor, header/footer
items, subagents, and one system-prompt slot. They can also request Kit-owned
confirm, input, and select dialogs, submit text to the active session, and open
HTTP(S) URLs. See the method tables and payload examples in the protocol spec.

Command, chrome, and subagent ids supplied by a plugin are local ids. Kit
prefixes the manifest id for ownership while presenting commands by local id:

- plugin `speech`, command `toggle` → canonical `speech.toggle`, presented as `/toggle`
- plugin `speech`, header item `status` → `speech.status`
- plugin `speech`, tool `speak_text` → canonical `speech.speak_text` and model
  name `speech__speak_text`

Header/footer styles use documented theme token names such as `toolText`, not
literal color values. Kit resolves tokens when rendering, so plugin content
tracks theme changes automatically.

Tool input schemas use the restricted JSON Schema Draft 2020-12 profile in the
protocol spec. Kit validates model input without coercion, defaults, property
removal, or plugin-side argument preparation.

## Trust and failures

External processes inherit Kit's environment and run as the same OS user. They
are not sandboxed and can access the filesystem, network, and subprocesses.
Only install plugins and open projects you trust.

Stdout must contain only UTF-8 newline-delimited JSON-RPC. Human-readable logs
belong on stderr. Invalid protocol output, initialization failure, or an
unexpected process exit removes all owned contributions and produces a
persistent manually dismissed error toast. Kit does not automatically restart a
failed plugin.

## Reloading and project changes

Use `/reload` after changing a plugin. Kit shuts down project plugins and then
user plugins in reverse load order, rediscovers manifests, and starts fresh
processes in normal order.

User plugin processes persist when the active cwd or session changes. On a cwd
change, Kit removes and shuts down only the old project plugins, sends
`kit/events/project.changed` to user plugins, and discovers project plugins for
the new cwd.

During normal shutdown Kit removes contributions immediately, requests
`shutdown`, waits up to two seconds for the process to exit, and then terminates
it if necessary.
