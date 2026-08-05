#!/usr/bin/env python3
"""Manage project .kitignore entries from slash commands."""
import json
import os
import sys
import threading

MAX_FRAME = 16 * 1024 * 1024


class Endpoint:
    def __init__(self):
        self.lock = threading.Lock()
        self.pending = {}
        self.next_id = 1
        self.cwd = os.getcwd()
        self.stopping = threading.Event()

    def send(self, message):
        data = json.dumps(message, separators=(",", ":"))
        if len(data.encode()) > MAX_FRAME:
            raise RuntimeError("Outbound frame exceeds 16 MiB")
        with self.lock:
            sys.stdout.write(data + "\n")
            sys.stdout.flush()

    def request(self, method, params=None):
        request_id = f"plugin-{self.next_id}"
        self.next_id += 1
        event, slot = threading.Event(), {}
        self.pending[request_id] = (event, slot)
        message = {"jsonrpc": "2.0", "id": request_id, "method": method}
        if params is not None:
            message["params"] = params
        self.send(message)
        event.wait()
        if "error" in slot:
            raise RuntimeError(slot["error"].get("message", "Kit request failed"))
        return slot.get("result")

    def notify(self, method, params):
        self.send({"jsonrpc": "2.0", "method": method, "params": params})

    def toast(self, title, variant, subtitle=None):
        params = {"title": title, "variant": variant}
        if subtitle:
            params["subtitle"] = subtitle
        self.notify("kit/ui/toast", params)

    def initialize(self, params):
        if params.get("protocolVersion") != 1:
            raise ValueError("Unsupported protocol version")
        self.cwd = params["context"]["project"]["cwd"]
        return {"protocolVersion": 1}

    def after_initialize(self):
        self.request("kit/commands/register", {"id": "ignore", "description": "Add a file or directory to .kitignore", "argName": "path"})
        self.request("kit/commands/register", {"id": "unignore", "description": "Remove a file or directory from .kitignore", "argName": "path"})

    def execute(self, params):
        command, raw = params.get("id"), params.get("args", "").strip()
        if not raw:
            self.toast(f"Usage: /file-ignore.{command} <path>", "warning")
            return None
        cleaned = raw.removeprefix("@").strip().rstrip("/")
        target = os.path.abspath(os.path.join(self.cwd, cleaned))
        if os.path.commonpath((self.cwd, target)) != self.cwd:
            self.toast("Path must be inside the current session directory", "warning")
            return None
        if command == "ignore":
            self.ignore(cleaned, target)
        elif command == "unignore":
            self.unignore(cleaned, target)
        else:
            raise KeyError("Unknown command")
        return None

    def nearest_ignore(self, anchor):
        current = anchor
        while os.path.commonpath((self.cwd, current)) == self.cwd:
            candidate = os.path.join(current, ".kitignore")
            if os.path.isfile(candidate):
                return candidate
            if current == self.cwd:
                break
            current = os.path.dirname(current)
        return os.path.join(self.cwd, ".kitignore")

    @staticmethod
    def normalized(line):
        line = line.strip()
        if not line or line.startswith("#"):
            return ""
        directory = line.endswith("/")
        line = line.rstrip("/").replace("\\", "/").removeprefix("./").strip("/")
        return line + ("/" if directory else "")

    def ignore(self, cleaned, target):
        if not os.path.isfile(target) and not os.path.isdir(target):
            self.toast(f"Path not found: {cleaned}", "warning")
            return
        directory = os.path.isdir(target)
        ignore_file = self.nearest_ignore(target if directory else os.path.dirname(target))
        entry = os.path.relpath(target, os.path.dirname(ignore_file)).replace("\\", "/") + ("/" if directory else "")
        try:
            with open(ignore_file, encoding="utf-8") as stream:
                existing = stream.read()
        except OSError:
            existing = ""
        if entry in {self.normalized(line) for line in existing.splitlines()}:
            self.toast("Already ignored", "info", entry)
            return
        created = not os.path.exists(ignore_file)
        with open(ignore_file, "a", encoding="utf-8") as stream:
            stream.write(("\n" if existing and not existing.endswith("\n") else "") + entry + "\n")
        location = os.path.relpath(ignore_file, self.cwd)
        self.toast(f"{'Created' if created else 'Updated'} {location}", "info", entry + "\nRun /reload if file suggestions were already scanned.")

    def unignore(self, cleaned, target):
        anchor = target if os.path.isdir(target) else os.path.dirname(target)
        current = anchor
        while os.path.commonpath((self.cwd, current)) == self.cwd:
            ignore_file = os.path.join(current, ".kitignore")
            try:
                with open(ignore_file, encoding="utf-8") as stream:
                    lines = stream.read().splitlines()
            except OSError:
                lines = []
            relative = os.path.relpath(target, current).replace("\\", "/")
            candidates = {relative, relative + "/"}
            removed = next((item for item in candidates if item in {self.normalized(line) for line in lines}), None)
            if removed:
                kept = [line for line in lines if self.normalized(line) != removed]
                with open(ignore_file, "w", encoding="utf-8") as stream:
                    stream.write("\n".join(kept).rstrip("\n") + "\n")
                self.toast(f"Removed {removed} from {os.path.relpath(ignore_file, self.cwd)}", "info", "Run /reload if file suggestions were already scanned.")
                return
            if current == self.cwd:
                break
            current = os.path.dirname(current)
        self.toast(f"No ignore entry found for: {cleaned}", "warning")

    def handle(self, message):
        if "method" not in message:
            pending = self.pending.pop(message.get("id"), None)
            if pending:
                pending[1].update(message)
                pending[0].set()
            return
        method, params = message["method"], message.get("params", {})
        if "id" not in message:
            if method == "kit/events/project.changed":
                self.cwd = params["cwd"]
            return
        try:
            if method == "initialize":
                result = self.initialize(params)
            elif method == "shutdown":
                result = None
                self.stopping.set()
            elif method == "kit/commands/execute":
                result = self.execute(params)
            else:
                raise KeyError("Method not found")
            self.send({"jsonrpc": "2.0", "id": message["id"], "result": result})
            if method == "initialize":
                self.after_initialize()
        except KeyError as error:
            self.send({"jsonrpc": "2.0", "id": message["id"], "error": {"code": -32601, "message": str(error)}})
        except Exception as error:
            print(error, file=sys.stderr)
            self.send({"jsonrpc": "2.0", "id": message["id"], "error": {"code": -32000, "message": str(error)}})

    def run(self):
        for line in sys.stdin:
            if len(line.encode()) > MAX_FRAME:
                break
            if not line.strip():
                continue
            try:
                frame = json.loads(line)
                for message in frame if isinstance(frame, list) else [frame]:
                    if message.get("method") == "shutdown":
                        self.handle(message)
                    else:
                        threading.Thread(target=self.handle, args=(message,), daemon=True).start()
            except Exception as error:
                print(error, file=sys.stderr)
            if self.stopping.is_set():
                break


Endpoint().run()
