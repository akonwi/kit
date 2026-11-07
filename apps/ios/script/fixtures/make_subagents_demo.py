"""Sets up an isolated Kit home for script/test_subagents.sh: five subagent
definitions and a small git project. Then, once that home's server is
running against subagents_model.py, starts a session whose main agent
starts the subagents:

    python3 apps/ios/script/fixtures/make_subagents_demo.py setup /tmp/kit-ios-agents /tmp/kit-ios-agents-demo
    python3 apps/ios/script/fixtures/subagents_model.py 60931 &
    KIT_HOME=/tmp/kit-ios-agents OPENAI_API_KEY=stand-in OPENAI_BASE_URL=http://127.0.0.1:60931/v1/ \\
        kit server run --listen 127.0.0.1:60932 &
    KIT_HOME=/tmp/kit-ios-agents python3 apps/ios/script/fixtures/make_subagents_demo.py session /tmp/kit-ios-agents-demo

The session is titled "Fix SSE reconnect". Running `session` again replaces
an earlier one, dismissing its subagents first.
"""
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

TITLE = "Fix SSE reconnect"

AGENTS = {
    "reviewer": ("Reviews code changes for regressions.", "openai/gpt-5.5"),
    "tester": ("Runs the test suites and reports failures.", "openai/gpt-5.4"),
    "scout": ("Finds the code a change touches.", "openai/gpt-5.4-mini"),
    "librarian": ("Searches project notes and history.", "openai/gpt-5.4-mini"),
    "designer": ("Reviews UI changes against the design language.", "openai/gpt-5.5"),
}


def refuse_default(home):
    if os.path.realpath(home) == os.path.realpath(os.path.expanduser("~/.kit")):
        sys.exit("Refusing the default Kit home; use an isolated one.")


def setup(home, project):
    refuse_default(home)
    if os.path.exists(project):
        sys.exit(f"{project} exists; pass a new folder.")
    os.makedirs(os.path.join(home, "agents"), exist_ok=True)
    for name, (description, model) in AGENTS.items():
        with open(os.path.join(home, "agents", f"{name}.md"), "w") as file:
            # The stand-in model reads the marker to know which agent it plays.
            file.write(f"---\nname: {name}\ndescription: {description}\nmodel: {model}\n---\nAGENT-ROLE {name}. {description}\n")
    for folder in ["Sources/Client", "docs"]:
        os.makedirs(os.path.join(project, folder))
    files = {
        "README.md": "# Client\n\nA client for the Kit session API.\n",
        "Sources/Client/HTTPClient.swift": "/// Opens the event stream from the cursor.\nfinal class EventStream {\n    var cursor: String?\n}\n",
        "docs/reconnect.md": "# Reconnecting\n\nThe server keeps 24 hours of events. A client further behind reloads a snapshot.\n",
    }
    for path, text in files.items():
        with open(os.path.join(project, path), "w") as file:
            file.write(text)
    subprocess.run(["git", "init", "-q"], cwd=project, check=True)
    subprocess.run(["git", "add", "."], cwd=project, check=True)
    subprocess.run(["git", "-c", "user.name=Kit", "-c", "user.email=kit@example.com", "commit", "-qm", "Start"], cwd=project, check=True)
    print(f"Set up {home} and {project}.")


def session(project):
    home = os.environ.get("KIT_HOME") or sys.exit("Set KIT_HOME to the isolated Kit home whose server is running.")
    refuse_default(home)
    with open(os.path.join(home, "run", "server.json")) as file:
        server = json.load(file)
    with open(os.path.join(home, "run", "server.token")) as file:
        token = file.read().strip()
    headers = {
        "Authorization": f"Bearer {token}",
        "X-Kit-Instance-ID": server["instanceId"],
        "X-Kit-Protocol-Version": str(server["protocolVersion"]),
        "Content-Type": "application/json",
    }

    def call(method, path, body=None, timeout=30, retry_conflict=False):
        data = None if body is None else json.dumps(body).encode()
        for attempt in range(40):
            request = urllib.request.Request(server["url"] + path, data=data, method=method, headers=headers)
            try:
                with urllib.request.urlopen(request, timeout=timeout) as response:
                    text = response.read()
                    return json.loads(text) if text else None
            except urllib.error.HTTPError as error:
                if retry_conflict and error.code == 409 and attempt < 39:
                    time.sleep(0.5)
                    continue
                sys.exit(f"{method} {path}: {error.code} {error.read().decode()}")

    # A session with subagents at work can't be deleted, so dismiss them first.
    for listed in call("GET", "/v1/sessions").get("sessions") or []:
        if (listed.get("name") or listed.get("title")) != TITLE:
            continue
        snapshot = call("GET", f"/v1/sessions/{listed['id']}")
        for conversation in snapshot.get("subagentConversations") or []:
            call("POST", f"/v1/sessions/{listed['id']}/subagents",
                 {"action": "dismiss", "conversationId": conversation["id"], "generation": conversation["generation"]})
        # Dismissed work takes a moment to stop.
        call("DELETE", f"/v1/sessions/{listed['id']}", retry_conflict=True)

    created = call("POST", "/v1/sessions", {"cwd": os.path.realpath(project), "name": TITLE,
                                            "model": "openai/gpt-5.5", "thinkingLevel": "high"})
    identifier = created["id"]
    call("POST", f"/v1/sessions/{identifier}/turns/prompt",
         {"text": "Get a review of the resume change and run the client tests before I merge."}, timeout=120)
    for _ in range(40):  # The subagents start working shortly after.
        snapshot = call("GET", f"/v1/sessions/{identifier}")
        if len(snapshot.get("subagentConversations") or []) == len(AGENTS):
            break
        time.sleep(0.5)
    print(identifier)


if __name__ == "__main__":
    command = sys.argv[1] if len(sys.argv) > 1 else ""
    if command == "setup" and len(sys.argv) == 4:
        setup(sys.argv[2], sys.argv[3])
    elif command == "session" and len(sys.argv) == 3:
        session(sys.argv[2])
    else:
        sys.exit(__doc__)
