"""Test-only, dependency-free interceptor; never installed automatically."""
import json
import sys

pending = {}


def send(value):
    print(json.dumps(value, separators=(",", ":")), flush=True)


def request(identifier, method, params=None):
    value = {"jsonrpc": "2.0", "id": identifier, "method": method}
    if params is not None:
        value["params"] = params
    send(value)


def reply(identifier, result):
    send({"jsonrpc": "2.0", "id": identifier, "result": result})


for line in sys.stdin:
    frame = json.loads(line)
    for message in frame if isinstance(frame, list) else [frame]:
        method = message.get("method")
        identifier = message.get("id")
        if method == "initialize":
            reply(identifier, {"protocolVersion": 1})
            request("register", "kit/tool-calls/register-interceptor")
        elif method == "shutdown":
            reply(identifier, None)
            sys.exit(0)
        elif method == "kit/tool-calls/before-execute":
            dialog = "approve:" + str(identifier)
            pending[dialog] = identifier
            request(dialog, "kit/ui/confirm", {"title": "Allow intercepted tool?", "message": message["params"]["toolCall"]["name"]})
        elif method == "kit/cancel":
            original = message["params"]["id"]
            for dialog, call in list(pending.items()):
                if call == original:
                    del pending[dialog]
                    send({"jsonrpc": "2.0", "method": "kit/cancel", "params": {"id": dialog}})
                    send({"jsonrpc": "2.0", "id": original, "error": {"code": -32001, "message": "Request cancelled"}})
        elif method == "kit/commands/execute":
            reply(identifier, None)
        elif method is None and identifier == "register":
            if "error" in message:
                sys.exit(1)
            request("ready", "kit/commands/register", {"id": "ready", "description": "Interceptor initialized"})
        elif method is None and identifier in pending:
            original = pending.pop(identifier)
            if "error" in message:
                send({"jsonrpc": "2.0", "id": original, "error": message["error"]})
            else:
                reply(original, {"action": "allow"} if message.get("result") is True else {"action": "reject-and-continue", "message": "User rejected intercepted tool"})
