"""A stand-in for the OpenAI Responses API that answers each message with a
reply: a long one, several screens tall, when the message asks for "long",
and a one-line one otherwise. A reply starts with the message's #tag, so a
test can tell its reply from earlier ones. It lets script/test_long_reply.sh run turns
without a real model or API key.

    python3 apps/ios/script/fixtures/long_reply_model.py 60932

Point an isolated Kit server at it with OPENAI_BASE_URL=http://127.0.0.1:60932/v1/
and OPENAI_API_KEY=stand-in.
"""
import json
import re
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LONG = "LONG-REPLY-TOP {tag} The reconnect drops events because the cursor is read before the stream is open.\n\n" + "\n\n".join(
    f"{n}. Step {n} of the fix: keep the cursor from the last event the client applied, and resume the stream from it "
    f"so nothing between the drop and the reconnect is lost or applied twice."
    for n in range(1, 31)
) + "\n\nLONG-REPLY-END That covers it."

SHORT = "SHORT-REPLY {tag} Done."


def last_user_text(body):
    """The text of the last user message in a request."""
    for item in reversed(body.get("input") or []):
        if item.get("role") == "user":
            content = item.get("content")
            if isinstance(content, str):
                return content
            return " ".join(part.get("text", "") for part in content or [] if isinstance(part, dict))
    return ""


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
            return self.stream(model, "Long reply demo")
        # A moment of work first, as a real model takes, so the reply arrives
        # while the reader waits at the end.
        time.sleep(1.5)
        message = last_user_text(body)
        tag = (re.search(r"#(\w+)", message) or [None, ""])[1]
        self.stream(model, (LONG if "long" in message.lower() else SHORT).replace("{tag}", tag))

    def stream(self, model, text):
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
        item = {"id": f"msg_{time.time_ns()}", "type": "message", "role": "assistant", "status": "completed",
                "content": [{"type": "output_text", "text": text, "annotations": []}]}
        send({"type": "response.output_text.delta", "item_id": item["id"], "output_index": 0, "content_index": 0, "delta": text})
        send({"type": "response.output_item.done", "output_index": 0, "item": item})
        send({"type": "response.completed", "response": {"id": response_id, "object": "response", "created_at": int(time.time()),
              "model": model, "status": "completed", "output": [item],
              "usage": {"input_tokens": 1200, "input_tokens_details": {"cached_tokens": 0}, "output_tokens": 400,
                        "output_tokens_details": {"reasoning_tokens": 0}, "total_tokens": 1600}}})


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 60932
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
