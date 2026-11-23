"""A stand-in for the OpenAI Responses API that asks the person the four
kinds of model questions in turn, each with a detail: a confirmation, a typed
answer, a choice, and guided questions. Each answer or decline moves to the
next. It lets script/test_dock_detail.sh show the questions without a real
model or API key.

    python3 apps/ios/script/fixtures/questions_model.py 60951

Point an isolated Kit server at it with OPENAI_BASE_URL=http://127.0.0.1:60951/v1/
and OPENAI_API_KEY=stand-in.
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# One request per model step, each with a detail. The guided questions have a
# detail for the whole set and one for a question.
CALLS = [
    ("confirm_from_user", {"title": "Push the branch?", "detail": "It is 3 commits ahead of origin/main."}),
    ("input_from_user", {"title": "Name the release", "detail": "Use the version, like v0.67.0."}),
    ("select_from_user", {"title": "Which build?", "detail": "Pick where the build goes.",
                          "options": [{"label": "TestFlight", "value": "testflight", "detail": "Internal testers get it."},
                                      {"label": "Phone", "value": "phone"}]}),
    ("guided_questions", {"title": "Release setup", "detail": "Two questions before the release.", "questions": [
        {"id": "notes", "prompt": "Release notes?", "detail": "One line is enough.", "kind": "text", "required": True},
        {"id": "ship", "prompt": "Ship it?", "kind": "boolean"}]}),
]


def steps_taken(body):
    """How many tool results the turn has had since its last user message."""
    count = 0
    for item in body.get("input") or []:
        kind = item.get("type") or ("message" if "role" in item else "")
        if kind == "message" and item.get("role") == "user":
            count = 0
        elif kind == "function_call_output":
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
        if not body.get("tools"):
            # Not a turn, such as a request for the session's title.
            return self.stream(body, None, "Questions demo")
        step = steps_taken(body)
        if step < len(CALLS):
            return self.stream(body, CALLS[step], None)
        return self.stream(body, None, "That's all I needed.")

    def stream(self, body, call, text):
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
        model = body.get("model", "gpt-4.1-mini")
        send({"type": "response.created", "response": {"id": response_id, "object": "response", "created_at": int(time.time()),
                                                        "model": model, "status": "in_progress", "output": []}})
        if call:
            tool, arguments = call
            item = {"id": f"fc_{time.time_ns()}", "type": "function_call", "call_id": f"call_{time.time_ns()}",
                    "name": tool, "arguments": json.dumps(arguments), "status": "completed"}
        else:
            item = {"id": f"msg_{time.time_ns()}", "type": "message", "role": "assistant", "status": "completed",
                    "content": [{"type": "output_text", "text": text, "annotations": []}]}
            send({"type": "response.output_text.delta", "item_id": item["id"], "output_index": 0,
                  "content_index": 0, "delta": text})
        send({"type": "response.output_item.done", "output_index": 0, "item": item})
        send({"type": "response.completed", "response": {"id": response_id, "object": "response", "created_at": int(time.time()),
              "model": model, "status": "completed", "output": [item],
              "usage": {"input_tokens": 100, "input_tokens_details": {"cached_tokens": 0}, "output_tokens": 10,
                        "output_tokens_details": {"reasoning_tokens": 0}, "total_tokens": 110}}})


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 60951
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
