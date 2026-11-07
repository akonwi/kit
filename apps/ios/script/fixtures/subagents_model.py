"""A stand-in for the OpenAI Responses API that plays one scripted run: the
main agent starts five subagents, which each read a file and then finish,
fail, or keep working. It lets script/test_subagents.sh show subagents
without a real model or API key.

    python3 apps/ios/script/fixtures/subagents_model.py 60931

Point an isolated Kit server at it with OPENAI_BASE_URL=http://127.0.0.1:60931/v1/
and OPENAI_API_KEY=stand-in; see make_subagents_demo.py.
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# What each subagent does, one step per model request. A step is a list of
# ("call", tool, arguments) and ("text", reply) outputs, or [("fail", message)].
# A step with a call continues the turn; one with only text ends it.
AGENTS = {
    "reviewer": [
        [("call", "read", {"path": "Sources/Client/HTTPClient.swift"})],
        [("text", "The cursor survives a reconnect now. One risk: when the server has pruned events older than the cursor it answers 410, and the client retries forever instead of reloading a snapshot.\n\nRunning the client tests to confirm."),
         ("call", "bash", {"command": "sleep 3600 # swift test --filter HTTPClient", "timeout": 3600000})],
        [("text", "The client tests pass.")],
    ],
    "scout": [
        [("call", "read", {"path": "README.md"})],
        [("call", "bash", {"command": "sleep 4 # find . -name '*.swift'"})],
        [("text", "Sources/Client/HTTPClient.swift is the only file that touches the event stream.")],
    ],
    "tester": [
        [("fail", "The simulator failed to boot.")],
    ],
    "librarian": [
        [("call", "read", {"path": "docs/reconnect.md"})],
        [("text", "The earlier reconnect notes are in docs/reconnect.md: the server keeps 24 hours of events, and a client further behind reloads a snapshot.")],
    ],
    "designer": [
        [("text", "The reconnect line should stay above the composer, as it does now.")],
    ],
}

TASKS = {
    "reviewer": "Review the SSE resume change in Sources/Client/HTTPClient.swift for regressions. Focus on reconnects and cursor handling.",
    "tester": "Run the client tests on a simulator and report failures.",
    "scout": "List the Swift files that touch the event stream.",
    "librarian": "Find the earlier notes on reconnecting.",
    "designer": "Check where the reconnect line sits in the session screen.",
}


def role(body):
    """The subagent a request is for, from the marker in its instructions, or None for Main."""
    text = json.dumps(body)
    for name in AGENTS:
        if f"AGENT-ROLE {name}." in text:
            return name
    return None


def steps_taken(body):
    """How many replies this conversation has had since its last user message."""
    count = 0
    for item in body.get("input") or []:
        kind = item.get("type") or ("message" if "role" in item else "")
        if kind == "message" and item.get("role") == "user":
            count = 0
        elif kind == "function_call_output" or (kind == "message" and item.get("role") == "assistant"):
            count += 1
    return count


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = json.loads(self.rfile.read(length) or b"{}")
        if not self.path.endswith("/responses"):
            self.send_error(404)
            return
        model = body.get("model", "gpt-5.5")
        if not body.get("tools"):
            # Not a turn, such as a request for the session's title.
            return self.stream(model, [("text", "Subagents demo")])
        name = role(body)
        step = steps_taken(body)
        if name is None:
            # Main starts the subagents once; their results come back as
            # later turns, which it only acknowledges.
            if any(item.get("type") == "function_call" for item in body.get("input") or []) and step == 0:
                return self.stream(model, [("text", "Noted. I’ll wait for the others.")])
            if step == 0:
                calls = [("call", "subagent", {"action": "start", "agent": agent, "message": TASKS[agent]})
                         for agent in ["reviewer", "tester", "scout", "librarian", "designer"]]
                return self.stream(model, calls)
            return self.stream(model, [("text", "I’ve asked reviewer to check the change, tester to run the suite, scout to list the stream code, librarian to find the earlier reconnect notes, and designer to check the reconnect line.")])
        script = AGENTS[name]
        actions = script[min(step, len(script) - 1)]
        if actions[0][0] == "fail":
            payload = json.dumps({"error": {"message": actions[0][1], "type": "invalid_request_error", "code": "stand_in"}}).encode()
            self.send_response(400)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        self.stream(model, actions)

    def stream(self, model, actions):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        sequence = 0

        def send(event):
            nonlocal sequence
            event["sequence_number"] = sequence
            sequence += 1
            self.wfile.write(f"data: {json.dumps(event)}\n\n".encode())
            self.wfile.flush()

        response_id = f"resp_{time.time_ns()}"
        send({"type": "response.created", "response": {"id": response_id, "object": "response", "created_at": int(time.time()),
                                                        "model": model, "status": "in_progress", "output": []}})
        output = []
        for index, action in enumerate(actions):
            if action[0] == "call":
                _, tool, arguments = action
                item = {"id": f"fc_{time.time_ns()}_{index}", "type": "function_call", "call_id": f"call_{time.time_ns()}_{index}",
                        "name": tool, "arguments": json.dumps(arguments), "status": "completed"}
            else:
                text = action[1]
                item = {"id": f"msg_{time.time_ns()}_{index}", "type": "message", "role": "assistant", "status": "completed",
                        "content": [{"type": "output_text", "text": text, "annotations": []}]}
                send({"type": "response.output_text.delta", "item_id": item["id"], "output_index": index,
                      "content_index": 0, "delta": text})
            send({"type": "response.output_item.done", "output_index": index, "item": item})
            output.append(item)
        send({"type": "response.completed", "response": {"id": response_id, "object": "response", "created_at": int(time.time()),
              "model": model, "status": "completed", "output": output,
              "usage": {"input_tokens": 1200, "input_tokens_details": {"cached_tokens": 0}, "output_tokens": 80,
                        "output_tokens_details": {"reasoning_tokens": 0}, "total_tokens": 1280}}})


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 60931
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
