# Tool demo

Requires Python 3 on `PATH`. This read-only v1 subprocess plugin registers
`tool-demo.echo`, exposed to the model as `tool-demo__echo`.

Reload the session after changing the fixture, then ask the model:

> Use tool-demo__echo to return exactly "plugin tools work".

The tool accepts one string `text` and returns it as model-visible text, plus
structured character-count details. It performs no filesystem writes or model
calls itself. Registration happens in the background and becomes visible at the
next model-request boundary; no additional reload is needed after initialization.

The model chooses whether to invoke a tool. The daemon integration test uses a
deterministic provider to verify registration, invocation, and durable results
without depending on a live model. A live-session smoke check invoked the tool
and returned `plugin tools work`. Broader manual lifecycle verification remains
outstanding.

Interception, separate prompt-slot contributions, and plugin subagents are not
part of this fixture or this implementation slice.
