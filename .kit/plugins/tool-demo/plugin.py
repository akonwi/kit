"""Read-only model tool fixture for the subprocess-v1 protocol."""
import json
import sys


def send(message):
    print(json.dumps({"jsonrpc": "2.0", **message}), flush=True)


def reply(request, result=None):
    send({"id": request["id"], "result": result})


for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    params = message.get("params") or {}
    if method == "initialize":
        reply(message, {"protocolVersion": 1})
        send({"id": "register-echo", "method": "kit/tools/register", "params": {
            "id": "echo", "label": "Plugin echo", "description": "Echo text through the read-only plugin tool demo.",
            "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}},
                            "required": ["text"], "additionalProperties": False},
            "executionMode": "parallel", "promptSnippet": "Use tool-demo__echo when asked to verify the plugin tool demo.",
            "promptGuidelines": ["This demonstration tool does not modify files."]
        }})
    elif method == "kit/tools/execute" and params.get("id") == "echo":
        text = params["input"]["text"]
        reply(message, {"content": [{"type": "text", "text": text}],
                        "details": {"characters": len(text)}, "terminate": False})
    elif method == "shutdown":
        reply(message)
        break
    elif method and "id" in message:
        send({"id": message["id"], "error": {"code": -32601, "message": "Unknown method"}})
