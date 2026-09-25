#!/usr/bin/env python3
"""Speak completed assistant responses on macOS."""

import json
import platform
import re
from collections import deque
import subprocess
import sys
import threading

MAX_FRAME_BYTES = 16 * 1024 * 1024
MAX_CHARS = 220
VOICE = None


class Endpoint:
    def __init__(self):
        self.write_lock = threading.Lock()
        self.state_lock = threading.Lock()
        self.toggle_lock = threading.Lock()
        self.pending = {}
        self.sequence = 0
        self.stopping = threading.Event()
        self.enabled = False
        self.state_generation = 0
        self.children = set()
        self.spoken_turns = set()
        self.spoken_turn_order = deque()

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

    def notify(self, method, params):
        self.send({"jsonrpc": "2.0", "method": method, "params": params})

    def render(self, enabled):
        self.request(
            "kit/footer/set",
            {
                "id": "status",
                "content": [
                    {"text": "speech ", "style": {"fg": "textMuted"}},
                    {
                        "text": "on" if enabled else "off",
                        "style": {
                            "fg": "toolText" if enabled else "textMuted",
                            "bold": enabled,
                        },
                    },
                ],
            },
        )

    def toggle(self):
        with self.toggle_lock:
            with self.state_lock:
                self.enabled = not self.enabled
                self.state_generation += 1
                enabled = self.enabled
                children = list(self.children) if not enabled else []
                if not enabled:
                    self.children.clear()
            for child in children:
                try:
                    child.terminate()
                except OSError:
                    pass
            self.render(enabled)
            return enabled

    def stop_speaking(self):
        with self.state_lock:
            self.enabled = False
            self.state_generation += 1
            children = list(self.children)
            self.children.clear()
        for child in children:
            try:
                child.terminate()
            except OSError:
                pass

    @staticmethod
    def shorten(text):
        text = re.sub(r"```[\s\S]*?```", " code block omitted ", text)
        text = re.sub(r"`([^`]+)`", r"\1", text)
        text = re.sub(r"\[(.*?)\]\((.*?)\)", r"\1", text)
        text = re.sub(r"[*_~#>]", "", text)
        text = re.sub(r"\s+", " ", text).strip()
        if len(text) <= MAX_CHARS:
            return text
        sentence = re.search(r"(.+?[.!?])(\s|$)", text)
        if sentence and len(sentence.group(1).strip()) <= MAX_CHARS:
            return sentence.group(1).strip()
        return text[: MAX_CHARS - 3] + "..."

    def forget_child(self, child):
        child.wait()
        with self.state_lock:
            self.children.discard(child)

    def completed(self, params):
        if platform.system() != "Darwin":
            return
        turn_id = params.get("turn", {}).get("id")
        if not isinstance(turn_id, str) or not turn_id:
            return
        with self.state_lock:
            if not self.enabled or turn_id in self.spoken_turns:
                return
            generation = self.state_generation
            if len(self.spoken_turn_order) == 1024:
                self.spoken_turns.discard(self.spoken_turn_order.popleft())
            self.spoken_turns.add(turn_id)
            self.spoken_turn_order.append(turn_id)
        text = None
        for message in reversed(params.get("turn", {}).get("messages", [])):
            if message.get("role") != "assistant":
                continue
            content = message.get("content", [])
            text = "\n".join(
                block["text"]
                for block in content
                if isinstance(block, dict)
                and block.get("type") == "text"
                and isinstance(block.get("text"), str)
            )
            break

        speech = self.shorten(text or "")
        if not speech:
            return
        try:
            args = ["say"] + (["-v", VOICE] if VOICE else []) + [speech]
            # Order the external side effect against disable and shutdown. Once
            # launch is admitted under this lock, either it remains enabled or
            # the state transition observes and terminates the registered child.
            with self.state_lock:
                if not self.enabled or generation != self.state_generation:
                    return
                child = subprocess.Popen(
                    args,
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                )
                self.children.add(child)
            threading.Thread(
                target=self.forget_child,
                args=(child,),
                daemon=True,
            ).start()
        except OSError as error:
            with self.state_lock:
                self.spoken_turns.discard(turn_id)
                try:
                    self.spoken_turn_order.remove(turn_id)
                except ValueError:
                    pass
            print(f"Could not launch macOS speech: {error}", file=sys.stderr)

    def bootstrap(self):
        self.render(False)
        self.request(
            "kit/commands/register",
            {
                "id": "toggle-speech",
                "description": "Toggle spoken assistant responses",
                "category": "plugins",
            },
        )

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
        if "id" not in message:
            if method == "kit/events/agent.turn.completed":
                self.completed(params)
            return

        bootstrap = False
        try:
            if method == "initialize":
                if params.get("protocolVersion") != 1:
                    raise ValueError("Unsupported protocol version")
                result = {"protocolVersion": 1}
                bootstrap = platform.system() == "Darwin"
            elif method == "shutdown":
                self.stop_speaking()
                result = None
                self.stopping.set()
            elif method == "kit/commands/execute" and params.get("id") == "toggle-speech":
                enabled = self.toggle()
                self.notify(
                    "kit/ui/toast",
                    {
                        "title": f"Speech {'enabled' if enabled else 'disabled'}",
                        "variant": "info",
                    },
                )
                result = None
            else:
                raise KeyError("Method not found")

            self.send({"jsonrpc": "2.0", "id": message["id"], "result": result})
        except KeyError as error:
            self.send(
                {
                    "jsonrpc": "2.0",
                    "id": message["id"],
                    "error": {"code": -32601, "message": str(error)},
                }
            )
            return
        except Exception as error:
            print(error, file=sys.stderr)
            self.send(
                {
                    "jsonrpc": "2.0",
                    "id": message["id"],
                    "error": {"code": -32000, "message": str(error)},
                }
            )
            return

        if bootstrap:
            try:
                self.bootstrap()
            except Exception as error:
                # The initialize request is already settled. Report bootstrap
                # failure only on stderr; a second response would violate JSON-RPC.
                print(f"Could not register speech contributions: {error}", file=sys.stderr)

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
