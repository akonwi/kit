"""stdio-v1 fixture for plugin URL requests.

The command replies immediately and requests the URL afterwards: the stdio
loop is synchronous, so a handler cannot wait for its own request's response.
Kit answers once it has validated and published the request, without waiting
for any client; each answer is reported as a toast.
"""

import json
import sys


def send(message):
    print(json.dumps({"jsonrpc": "2.0", **message}), flush=True)


def reply(request, result=None, error=None):
    if "id" in request:
        send({"id": request["id"], **({"error": error} if error else {"result": result})})


def toast(title, subtitle, variant="info"):
    send({"method": "kit/ui/toast", "params": {
        "title": title, "subtitle": subtitle, "variant": variant
    }})


requests = 0
pending = {}  # request id -> requested url


def open_url(url):
    global requests
    requests += 1
    request_id = "open-%d" % requests
    pending[request_id] = url
    send({"id": request_id, "method": "kit/system/open-url", "params": {"url": url}})


def settle(response):
    url = pending.pop(response["id"])
    if "error" in response:
        error = response["error"]
        toast("URL rejected", "%d %s" % (error["code"], error["message"]), "warning")
    else:
        toast("URL requested", url)


for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    params = message.get("params") or {}
    if method is None:
        if message.get("id") in pending:
            settle(message)
        continue
    if method == "initialize":
        reply(message, {"protocolVersion": 1})
        send({"id": "register-open", "method": "kit/commands/register", "params": {
            "id": "open", "description": "Ask the Mac app to open a URL in its browser", "argName": "url"
        }})
    elif method == "kit/commands/execute":
        reply(message)
        if params["id"] == "open":
            open_url(params["args"].strip())
    elif method == "shutdown":
        reply(message)
        break
    elif "id" in message:
        reply(message, error={"code": -32601, "message": "Unknown method"})
