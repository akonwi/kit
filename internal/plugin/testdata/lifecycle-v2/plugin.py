"""Test-only plugin for protocol 2 that withdraws what it registers.

Its churn command unregisters a command and its tool, registers and
unregisters an interceptor, and cancels a dialog it opened, so the conformance
test sees every plugin request the repository's fixtures do not send. Never
installed automatically.
"""
import json
import sys


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
    message = json.loads(line)
    method = message.get("method")
    identifier = message.get("id")
    if method == "initialize":
        reply(identifier, {"protocolVersion": 2})
        request("churn", "kit/commands/register", {"id": "churn", "description": "Withdraw contributions"})
        request("temporary", "kit/commands/register", {"id": "temporary", "description": "Removed by churn"})
        request("scratch", "kit/tools/register", {"id": "scratch", "description": "Removed by churn", "inputSchema": {"type": "object"}})
    elif method == "shutdown":
        reply(identifier, None)
        sys.exit(0)
    elif method == "kit/commands/execute":
        request("unregister-command", "kit/commands/unregister", {"id": "temporary"})
        request("unregister-tool", "kit/tools/unregister", {"id": "scratch"})
        request("intercept", "kit/tool-calls/register-interceptor")
        request("stop-intercepting", "kit/tool-calls/unregister-interceptor")
        request("dialog", "kit/ui/confirm", {"title": "Withdrawn at once"})
        send({"jsonrpc": "2.0", "method": "kit/cancel", "params": {"id": "dialog"}})
        reply(identifier, None)
    elif method == "kit/tools/execute":
        reply(identifier, {"content": [{"type": "text", "text": "scratch"}]})
