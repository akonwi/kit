"""Small stdio-v1 fixture for manual command, context, and toast testing."""

import json
import sys


def send(message):
    print(json.dumps({"jsonrpc": "2.0", **message}), flush=True)


def reply(request, result=None, error=None):
    if "id" in request:
        send({"id": request["id"], **({"error": error} if error else {"result": result})})


def toast(title, subtitle):
    send({"method": "kit/ui/toast", "params": {
        "title": title, "subtitle": subtitle, "variant": "info"
    }})


context = {}
for line in sys.stdin:
    request = json.loads(line)
    method = request.get("method")
    params = request.get("params") or {}
    if method == "initialize":
        context = params["context"]
        reply(request, {"protocolVersion": 1})
        for name, description, arg_name in [
            ("echo", "Show literal command arguments in a toast", "message"),
            ("context", "Show this plugin's current session and directory", None),
        ]:
            send({"id": "register-" + name, "method": "kit/commands/register", "params": {
                "id": name, "description": description, "argName": arg_name
            }})
    elif method == "kit/events/project.changed":
        context["project"] = params
    elif method == "kit/events/session.changed":
        context["session"] = params
    elif method == "kit/commands/execute":
        if params["id"] == "echo":
            # Toast subtitles are bounded by the v1 host profile.
            message = params["args"].encode("utf-8")[:4000].decode("utf-8", errors="ignore")
            toast("Plugin echo", message or "No arguments supplied")
            reply(request)
        elif params["id"] == "context":
            session = context["session"]
            toast("Plugin context", (session.get("name") or session["id"]) + "\n" + context["project"]["cwd"])
            reply(request)
        else:
            reply(request, error={"code": -32601, "message": "Unknown command"})
    elif method == "shutdown":
        reply(request)
        break
    elif method and "id" in request:
        reply(request, error={"code": -32601, "message": "Unknown method"})
