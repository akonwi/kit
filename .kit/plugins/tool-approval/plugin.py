#!/usr/bin/env python3
"""Ask before risky shell tool calls."""

import json
import re
import sys
import threading

MAX_FRAME_BYTES = 16 * 1024 * 1024
RISKY_PATTERNS = (r"\bgit\s+commit\b", r"\bnpm\s+publish\b")


class Endpoint:
    def __init__(self):
        self.write_lock = threading.Lock()
        self.state_lock = threading.Lock()
        self.pending = {}
        self.sequence = 0
        self.stopping = threading.Event()

    def send(self, message):
        data = json.dumps(message, separators=(",", ":"))
        if len(data.encode()) > MAX_FRAME_BYTES:
            raise RuntimeError("Outbound frame exceeds 16 MiB")
        with self.write_lock:
            sys.stdout.write(data + "\n")
            sys.stdout.flush()

    def request(self, method, params=None):
        event, result = threading.Event(), {}
        with self.state_lock:
            self.sequence += 1
            request_id = f"plugin-{self.sequence}"
            self.pending[request_id] = (event, result)
        message = {"jsonrpc": "2.0", "id": request_id, "method": method}
        if params is not None:
            message["params"] = params
        self.send(message)
        event.wait()
        if "error" in result:
            raise RuntimeError(result["error"].get("message", "Kit request failed"))
        return result.get("result")

    def approve(self, tool_call):
        command = tool_call.get("input", {}).get("command", "")
        risky = (
            tool_call.get("name") == "bash"
            and isinstance(command, str)
            and any(re.search(pattern, command) for pattern in RISKY_PATTERNS)
        )
        if not risky:
            return {"action": "allow"}

        approved = self.request(
            "kit/ui/confirm",
            {
                "title": f"Allow {tool_call['name']}?",
                "message": command or "(no command)",
                "confirmLabel": "Allow",
                "cancelLabel": "Block",
                "defaultValue": False,
            },
        )
        if approved:
            return {"action": "allow"}
        return {
            "action": "reject-and-continue",
            "message": f"The user rejected {tool_call['name']}.",
        }

    def handle(self, message):
        if "method" not in message:
            with self.state_lock:
                pending = self.pending.pop(message.get("id"), None)
            if pending:
                pending[1].update(message)
                pending[0].set()
            return
        if "id" not in message:
            return

        method = message["method"]
        params = message.get("params", {})
        try:
            if method == "initialize":
                if params.get("protocolVersion") != 1:
                    raise ValueError("Unsupported protocol version")
                result = {"protocolVersion": 1}
            elif method == "shutdown":
                result = None
                self.stopping.set()
            elif method == "kit/tool-calls/before-execute":
                result = self.approve(params["toolCall"])
            else:
                raise KeyError("Method not found")

            self.send({"jsonrpc": "2.0", "id": message["id"], "result": result})
            if method == "initialize":
                self.request("kit/tool-calls/register-interceptor")
        except KeyError as error:
            self.send(
                {
                    "jsonrpc": "2.0",
                    "id": message["id"],
                    "error": {"code": -32601, "message": str(error)},
                }
            )
        except Exception as error:
            print(error, file=sys.stderr)
            self.send(
                {
                    "jsonrpc": "2.0",
                    "id": message["id"],
                    "error": {"code": -32000, "message": str(error)},
                }
            )

    def run(self):
        for line in sys.stdin:
            if len(line.encode()) > MAX_FRAME_BYTES:
                break
            if not line.strip():
                continue
            try:
                frame = json.loads(line)
                for message in frame if isinstance(frame, list) else [frame]:
                    if message.get("method") == "shutdown":
                        self.handle(message)
                    else:
                        threading.Thread(
                            target=self.handle,
                            args=(message,),
                            daemon=True,
                        ).start()
            except Exception as error:
                print(error, file=sys.stderr)
            if self.stopping.is_set():
                break


Endpoint().run()
