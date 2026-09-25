#!/usr/bin/env python3
"""Require confirmation for selected high-impact repository commands."""

import json
import re
import sys
import threading
import time

MAX_FRAME_BYTES = 16 * 1024 * 1024
RISKY_BASH_PATTERNS = (
    re.compile(r"\bgit\s+commit\b"),
    re.compile(r"\bnpm\s+publish\b"),
)


class Endpoint:
    def __init__(self):
        self.write_lock = threading.Lock()
        self.state_lock = threading.Lock()
        self.pending = {}
        self.nested_requests = {}
        self.active_owners = set()
        self.cancelled_owners = set()
        self.sequence = 0
        self.stopping = threading.Event()

    def send(self, message):
        data = json.dumps(message, separators=(",", ":"))
        if len(data.encode()) > MAX_FRAME_BYTES:
            raise RuntimeError("Outbound frame exceeds 16 MiB")
        with self.write_lock:
            sys.stdout.write(data + "\n")
            sys.stdout.flush()

    def request(self, method, params=None, owner_id=None, delay_before_send=False):
        event, result = threading.Event(), {}
        with self.state_lock:
            if owner_id is not None and owner_id in self.cancelled_owners:
                self.cancelled_owners.discard(owner_id)
                raise RuntimeError("Request cancelled")
            self.sequence += 1
            request_id = f"plugin-{self.sequence}"
            self.pending[request_id] = (event, result)
            if owner_id is not None:
                self.nested_requests[owner_id] = {
                    "id": request_id,
                    "sent": False,
                    "cancelled": False,
                }
        message = {"jsonrpc": "2.0", "id": request_id, "method": method}
        if params is not None:
            message["params"] = params
        if delay_before_send:
            self.send(
                {
                    "jsonrpc": "2.0",
                    "method": "kit/ui/toast",
                    "params": {
                        "title": "Nested request registered",
                        "variant": "info",
                    },
                }
            )
            time.sleep(0.05)
        self.send(message)
        nested_cancel_id = None
        if owner_id is not None:
            with self.state_lock:
                nested = self.nested_requests.get(owner_id)
                if nested is not None and nested["id"] == request_id:
                    nested["sent"] = True
                    if nested["cancelled"]:
                        nested_cancel_id = request_id
        if nested_cancel_id is not None:
            self.send(
                {
                    "jsonrpc": "2.0",
                    "method": "kit/cancel",
                    "params": {"id": nested_cancel_id},
                }
            )
        event.wait()
        with self.state_lock:
            if owner_id is not None:
                self.nested_requests.pop(owner_id, None)
        if "error" in result:
            raise RuntimeError(result["error"].get("message", "Kit request failed"))
        return result.get("result")

    def register_owner(self, owner_id):
        with self.state_lock:
            self.active_owners.add(owner_id)

    def finish_owner(self, owner_id):
        with self.state_lock:
            self.active_owners.discard(owner_id)
            self.cancelled_owners.discard(owner_id)
            self.nested_requests.pop(owner_id, None)

    def cancel(self, owner_id):
        with self.state_lock:
            if owner_id not in self.active_owners:
                return
            nested = self.nested_requests.get(owner_id)
            nested_id = None
            if nested is None:
                self.cancelled_owners.add(owner_id)
            elif nested["sent"]:
                nested_id = nested["id"]
            else:
                nested["cancelled"] = True
        if nested_id is not None:
            self.send(
                {
                    "jsonrpc": "2.0",
                    "method": "kit/cancel",
                    "params": {"id": nested_id},
                }
            )

    def approve_tool_call(self, request_id, params):
        tool_call = params.get("toolCall", {})
        tool_name = tool_call.get("name", "tool")
        tool_input = tool_call.get("input", {})
        command = tool_input.get("command", "") if isinstance(tool_input, dict) else ""
        if tool_name != "bash" or not any(
            pattern.search(command) for pattern in RISKY_BASH_PATTERNS
        ):
            return {"action": "allow"}

        if "--delay-approval" in command:
            time.sleep(0.05)
        indented_command = "\n".join(f"    {line}" for line in command.splitlines())
        approved = self.request(
            "kit/ui/confirm",
            {
                "title": f"Allow {tool_name}?",
                "message": f"The tool requested this command:\n\n{indented_command}",
                "confirmLabel": "Allow",
                "cancelLabel": "Block",
                "defaultValue": False,
            },
            owner_id=request_id,
            delay_before_send="--delay-nested-send" in command,
        )
        if approved:
            return {"action": "allow"}
        return {
            "action": "reject-and-continue",
            "message": f"The user rejected {tool_name}.",
        }

    def handle(self, message):
        if "method" not in message:
            with self.state_lock:
                pending = self.pending.pop(message.get("id"), None)
            if pending:
                pending[1].update(message)
                pending[0].set()
            return

        method = message["method"]
        params = message.get("params", {})
        if method == "kit/cancel" and "id" not in message:
            self.cancel(params.get("id"))
            return
        if "id" not in message:
            return

        try:
            if method == "initialize":
                if params.get("protocolVersion") != 1:
                    raise ValueError("Unsupported protocol version")
                result = {"protocolVersion": 1}
            elif method == "shutdown":
                result = None
                self.stopping.set()
            elif method == "kit/tool-calls/before-execute":
                result = self.approve_tool_call(message["id"], params)
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
        finally:
            if method == "kit/tool-calls/before-execute":
                self.finish_owner(message["id"])

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
                        if message.get("method") == "kit/tool-calls/before-execute":
                            self.register_owner(message.get("id"))
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
