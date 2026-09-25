"""Manual plugin-provided subagent fixture."""
import json
import sys

sequence = 0
pending = {}

def send(message):
    print(json.dumps({"jsonrpc": "2.0", **message}), flush=True)

def call(method, params, owner=None):
    global sequence
    sequence += 1
    identity = f"subagent-{sequence}"
    send({"id": identity, "method": method, "params": params})
    if owner is not None:
        pending[identity] = owner

def reply(request, result=None):
    send({"id": request["id"], "result": result})

for line in sys.stdin:
    request = json.loads(line)
    method = request.get("method")
    params = request.get("params") or {}
    if method == "initialize":
        reply(request, {"protocolVersion": 1})
        call("kit/subagents/register", {
            "id": "reviewer",
            "description": "Reviews code changes for regressions",
            "instructions": "Inspect the relevant changes and return concrete findings.",
            "model": None,
        })
        call("kit/commands/register", {"id": "clear", "description": "Unregister the demo subagent"})
    elif method == "kit/commands/execute" and params.get("id") == "clear":
        call("kit/subagents/unregister", {"id": "reviewer"}, request)
    elif method == "shutdown":
        reply(request)
        break
    elif method and "id" in request:
        send({"id": request["id"], "error": {"code": -32601, "message": "Unknown method"}})
    elif request.get("id") in pending:
        owner = pending.pop(request["id"])
        if request.get("error"):
            send({"id": owner["id"], "error": request["error"]})
        else:
            reply(owner)
    elif request.get("error"):
        print(request["error"]["message"], file=sys.stderr)
