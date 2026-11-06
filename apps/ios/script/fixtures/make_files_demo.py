"""Makes the project script/test_files.sh opens files in: a long Swift file,
a line wider than the screen, a Markdown file, an Ard file, an empty file,
and a binary one, committed in a new git repository.

    python3 apps/ios/script/fixtures/make_files_demo.py /tmp/kit-ios-files-demo
"""
import os
import subprocess
import sys

root = sys.argv[1]
if os.path.exists(root):
    sys.exit(f"{root} exists; pass a new folder.")
for folder in ["Sources/Client", "docs", "std"]:
    os.makedirs(os.path.join(root, folder))


def write(path, text):
    with open(os.path.join(root, path), "w") as file:
        file.write(text)


lines = ["import Foundation", "", "/// A client for the Kit session API."]
for i in range(1, 1660):
    if i % 40 == 1:
        lines.append(f"// MARK: Section {i // 40 + 1}")
    elif i % 7 == 0:
        lines.append(f'    let value{i} = "string {i}" // note {i}')
    elif i % 5 == 0:
        lines.append(f"    func step{i}(_ input: Int) -> Int {{ input * {i} }}")
    else:
        lines.append(f"    var field{i}: Int = {i}")
lines += [
    "/// Opens the event stream from the cursor.", "final class EventStream {",
    "    private var cursor: EventCursor", "    private var stream: AsyncBytes?", "",
    "    init(cursor: EventCursor) {", "        self.cursor = cursor", "    }", "",
    "    func connect(_ request: inout Request) {", "        request.accept = .eventStream",
    "        request.cursor = cursor.last", "        if let id = cursor.last {",
    "            request.lastEventID = id", "        }", "        stream = try await open(request)", "    }", "",
    "    func close() {", "        stream = nil", "    }", "}",
]
write("Sources/Client/HTTPClient.swift", "\n".join(lines) + "\n")
write("Sources/Client/Wide.swift", 'let wide = "' + "a very long line that keeps going " * 12 + '"\n\tlet tabbed = 1\n')
write("docs/pairing.md", """# Pairing a device

A paired device holds its own credential for one server. Pair from the machine that runs the server, then enter the code on the device.

```sh
kit server pair
```

## Codes

- A code works once and expires after five minutes.
- Remove a device with `kit server devices`.

## Plain HTTP

Prefer HTTPS. A device on plain HTTP must be on a network you trust, and the app asks before it connects.
""")
write("std/list.ard", 'use ard/io\n\nfn greet(name: Str) Str {\n  let message = "Hello, {name}" // greet\n  message\n}\n')
write("README.md", "# Demo\n\nA project for the iOS Files test.\n")
write("empty.txt", "")
with open(os.path.join(root, "logo.png"), "wb") as file:
    file.write(bytes([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]) + bytes(range(256)))

git = ["git", "-C", root, "-c", "user.email=demo@example.com", "-c", "user.name=Demo"]
subprocess.run(git + ["init", "-q"], check=True)
subprocess.run(git + ["add", "-A"], check=True)
subprocess.run(git + ["commit", "-qm", "Add the demo files"], check=True)
