"""stdio-v1 fixture for plugin-submitted session messages.

Commands reply immediately and submit afterwards: the stdio loop is
synchronous, so a handler cannot wait for its own submission response.
Every submission outcome is reported as a toast.
"""

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


submissions = 0
pending = {}  # request id -> purpose
loop = {"remaining": 0, "total": 0, "turn": None}
# Turn completion may arrive before or after the submission response.
completed_turns = set()


def submit(text, purpose, key=None):
    global submissions
    submissions += 1
    request_id = "submit-%d" % submissions
    pending[request_id] = purpose
    params = {"text": text}
    if key is not None:
        params["idempotencyKey"] = key
    send({"id": request_id, "method": "kit/session/submit-message", "params": params})


def submit_next_loop_turn():
    index = loop["total"] - loop["remaining"] + 1
    loop["remaining"] -= 1
    loop["turn"] = None
    submit("Loop turn %d of %d." % (index, loop["total"]), "loop")


def settle(response):
    purpose = pending.pop(response["id"])
    if "error" in response:
        error = response["error"]
        toast("Message rejected", "%d %s" % (error["code"], error["message"]))
        return
    result = response["result"]
    toast("Message admitted", json.dumps(result, sort_keys=True))
    if purpose == "loop":
        loop["turn"] = result["turnId"]
        continue_loop()


def continue_loop():
    if loop["remaining"] > 0 and loop["turn"] in completed_turns:
        submit_next_loop_turn()


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
        for name, description, arg_name in [
            ("send", "Submit the arguments as a session message", "message"),
            ("send-keyed", "Submit the arguments with a fixed idempotency key", "message"),
            ("loop", "Submit a message after each completed loop turn", "count"),
        ]:
            send({"id": "register-" + name, "method": "kit/commands/register", "params": {
                "id": name, "description": description, "argName": arg_name
            }})
    elif method == "kit/commands/execute":
        command, args = params["id"], params["args"]
        reply(message)
        if command == "send":
            submit(args, "send")
        elif command == "send-keyed":
            submit(args, "send", key="demo-key")
        elif command == "loop":
            loop.update(remaining=int(args), total=int(args), turn=None)
            submit_next_loop_turn()
    elif method == "kit/events/agent.turn.completed":
        # Continue only after the turn this plugin started has completed.
        completed_turns.add(params["turn"]["id"])
        continue_loop()
    elif method == "shutdown":
        reply(message)
        break
    elif "id" in message:
        reply(message, error={"code": -32601, "message": "Unknown method"})
