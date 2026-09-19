#!/usr/bin/env python3
"""Exercise real terminal parsing and focus without credentials or a daemon.

Run from any directory: python3 scripts/test-tui-input-ownership.py
Builds the opt-in Go fixture, uses an isolated PTY/home, and removes its files.
"""

import fcntl
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time

ROOT = Path(__file__).resolve().parent.parent
STEPS = [
    (3, b"draft"),
    (3.5, b"\x1b[D\x1b[D"),
    (4, b"\x10"),  # Ctrl+P
    (4.5, b"model"),
    (5, b"\x1bOQ"),  # Fixture F2: interaction arrives beneath palette
    (5.5, b"X"),
    (6, b"\x1b"),
    (6.5, b"\x1b[200~answer\x1b[201~"),
    (7, b"\r"),
    (7.5, b"!"),  # Insert at the original draft cursor
    (8, b"\x1b[<0;2;28M\x1b[<0;2;28m"),  # Click composer start
    (8.5, b"X"),
    (9, b"\x03"),  # Clear the non-empty composer
    (9.5, b"\x03"),  # Empty composer: detach
]
REPLIES = [
    (b"\x1b[c", b"\x1b[?64;1;2;6;9;15;18;21;22c"),
    (b"\x1b[>c", b"\x1b[>0;276;0c"),
    (b"\x1b[6n", b"\x1b[1;1R"),
    (b"\x1b[?u", b"\x1b[?0u"),
    (b"\x1b]10;?", b"\x1b]10;rgb:dddd/dddd/dddd\x1b\\"),
    (b"\x1b]11;?", b"\x1b]11;rgb:1111/1111/1111\x1b\\"),
]


def run():
    with tempfile.TemporaryDirectory(prefix="kit-tui-smoke-") as directory:
        binary = str(Path(directory) / "tui.test")
        subprocess.run(
            ["go", "test", "-c", "-o", binary, "./internal/tui"],
            cwd=ROOT,
            check=True,
        )
        pid, fd = pty.fork()
        if pid == 0:
            os.environ.update(
                KIT_HOME=directory,
                TERM="xterm-256color",
                COLORTERM="truecolor",
                KIT_TUI_TERMINAL_SMOKE="1",
            )
            os.execv(binary, [binary, "-test.run=^TestTerminalInputOwnershipSmoke$", "-test.v"])
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        output = bytearray()
        started, step, status = time.monotonic(), 0, None
        try:
            while time.monotonic() - started < 20:
                if step < len(STEPS) and time.monotonic() - started > STEPS[step][0]:
                    os.write(fd, STEPS[step][1])
                    step += 1
                if select.select([fd], [], [], 0.05)[0]:
                    try:
                        data = os.read(fd, 65536)
                    except OSError:
                        break
                    if not data:
                        break
                    output.extend(data)
                    for query, reply in REPLIES:
                        if query in data:
                            os.write(fd, reply)
                done, result = os.waitpid(pid, os.WNOHANG)
                if done:
                    status = result
                    break
        finally:
            if status is None:
                done, status = os.waitpid(pid, os.WNOHANG)
                if not done:
                    os.kill(pid, signal.SIGTERM)
                    _, status = os.waitpid(pid, 0)
            os.close(fd)
        if step != len(STEPS) or os.waitstatus_to_exitcode(status) != 0 or b"--- PASS:" not in output:
            raise SystemExit("Terminal input smoke check failed:\n" + repr(bytes(output)))
        print("PASS: terminal palette/dock ownership, bracketed paste, cursor restoration, mouse, Ctrl+C clear/detach")


if __name__ == "__main__":
    run()
