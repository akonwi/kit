"""Manual static-footer fixture. Changes chrome only when a command is run."""
import json
import sys

sequence = 0
pending = {}

def send(message):
    print(json.dumps({"jsonrpc": "2.0", **message}), flush=True)

def call(method, params):
    global sequence
    sequence += 1
    identity = f"footer-{sequence}"
    send({"id": identity, "method": method, "params": params})
    return identity

def reply(request, result=None):
    send({"id": request["id"], "result": result})

def execute_steps(request, steps):
    if not steps:
        reply(request)
        return
    method, params = steps[0]
    identity = call(method, params)
    pending[identity] = (request, steps[1:])

for line in sys.stdin:
    request = json.loads(line)
    method = request.get("method")
    params = request.get("params") or {}
    if method == "initialize":
        reply(request, {"protocolVersion": 1})
        for name, description in [("set", "Set styled bottom-right footer text"),
                                  ("replace", "Hide cwd/Git and show demo footer text"),
                                  ("clear", "Remove demo footer text and restore cwd/Git")]:
            call("kit/commands/register", {"id": name, "description": description, "argName": "label"})
    elif method == "kit/commands/execute":
        name = params["id"]
        if name in ("set", "replace"):
            label = (params.get("args") or "Footer demo")[:512]
            steps = [("kit/footer/set", {"id": "status", "content": [{"text": label, "style": {"fg": "toolText", "bold": True}}]}),
                     ("kit/footer/hide" if name == "replace" else "kit/footer/show", {"id": "kit.footer.location"})]
        elif name == "clear":
            steps = [("kit/footer/clear", {"id": "status"}),
                     ("kit/footer/show", {"id": "kit.footer.location"})]
        else:
            send({"id": request["id"], "error": {"code": -32601, "message": "Unknown command"}})
            continue
        execute_steps(request, steps)
    elif method == "shutdown":
        reply(request)
        break
    elif method and "id" in request:
        send({"id": request["id"], "error": {"code": -32601, "message": "Unknown method"}})
    elif request.get("id") in pending:
        command, steps = pending.pop(request["id"])
        if request.get("error"):
            send({"id": command["id"], "error": request["error"]})
        else:
            execute_steps(command, steps)
    elif request.get("error"):
        print(request["error"]["message"], file=sys.stderr)
