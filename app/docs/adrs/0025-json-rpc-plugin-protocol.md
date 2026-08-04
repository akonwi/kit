# 0025: JSON-RPC external plugin protocol

## Status

Accepted

## Context

Kit currently loads external plugins as trusted TypeScript modules. It discovers
direct `*.ts` files under `~/.kit/plugins/` and `<cwd>/.kit/plugins/`, installs
their npm dependencies, bundles them with Bun, imports them into the Kit
process, and invokes a default-exported initializer with a function-based
`PluginAPI`.

That design gives plugins a capability-oriented boundary, but the loading and
authoring contract is coupled to TypeScript, npm package resolution, TypeBox,
Bun bundling, JavaScript callbacks, and in-process function disposers. A plugin
cannot be authored as a Python program, a Go or Rust binary, or another
language implementation without first wrapping it in TypeScript.

External plugins are also a different architectural concern from Kit's
built-in plugins. Built-ins are application code and may continue to use the
in-process internal plugin API. External plugins need a stable,
language-neutral process boundary.

## Decision

Replace the external TypeScript plugin loader and SDK with a versioned Kit
plugin protocol built on JSON-RPC 2.0 over stdio.

Built-in plugins remain in-process. The JSON-RPC protocol applies only to user
and project plugins discovered from:

1. `~/.kit/plugins/*/plugin.json`
2. `<cwd>/.kit/plugins/*/plugin.json`

Each immediate child directory is one plugin installation. A standalone plugin
repository may keep `plugin.json` at its root and be cloned or symlinked into a
Kit plugin directory.

The v1 manifest declares a stable plugin id and a stdio launch command. Stdio
is the only v1 transport. Kit launches one long-lived process per manifest,
uses stdin and stdout for newline-delimited JSON-RPC messages, and continuously
drains stderr for plugin diagnostics. Kit invokes commands, tools, tool-call
interceptors, click actions, lifecycle methods, and events through explicit
JSON-RPC methods. Plugins invoke Kit capabilities through the same full-duplex
connection.

The protocol is defined independently from TypeScript. Its normative artifacts
are:

- `docs/plugin-protocol/v1.md`
- `docs/plugin-protocol/manifest.schema.json`
- `docs/plugin-protocol/protocol.schema.json`

The JSON Schemas are the source for runtime validation and implementation
types. A TypeScript type definition or convenience SDK must not become the
source of truth for the wire contract.

## Identity and ownership

The manifest plugin id is the ownership boundary. Plugins register local
contribution ids, and Kit derives canonical ids by prefixing the plugin id.

For example, plugin `speech` registering command `toggle` owns canonical
command `speech.toggle`. Kit sends `toggle` back to that process when invoking
the command. No callback handler id or separate registration token exists.
Explicit unregistration uses the same local contribution id, and all remaining
contributions are removed automatically when the process disconnects.

Model-facing tool names replace the canonical separator `.` with `__`. Tool
`speech.speak_text` is therefore exposed to the model as
`speech__speak_text`.

## Lifecycle

Kit initializes plugins sequentially in deterministic discovery order. User
plugins load before project plugins. A failed plugin does not prevent later
plugins from starting.

The `initialize` request carries protocol version 1 and only the live context a
plugin starts within:

- project cwd and initial Git context
- active session id and nullable session name

Kit considers a plugin ready after it returns protocol version 1. Plugins may
not register contributions or invoke other Kit methods before initialization
succeeds. Initialization has a ten-second timeout.

User plugin processes persist across project cwd changes and receive context
events. Project plugin processes belong to the cwd from which they were
discovered; Kit shuts them down before leaving that cwd and discovers fresh
project plugins after retargeting.

`/reload` performs process replacement rather than module hot reload. Kit
removes contributions as shutdown starts, sends `shutdown`, allows two seconds
for graceful cleanup, and then terminates an unresponsive process. Kit does not
automatically restart crashed plugins in v1.

## Public surface

The v1 protocol supports:

- command registration, execution, and removal
- tool registration, execution, and removal using a documented JSON Schema
  profile
- one optional tool-call interceptor per plugin
- toast, confirm, input, and select UI capabilities
- header and footer contributions, click actions, and hide claims
- declarative subagent registration
- one replaceable system-prompt contribution per plugin
- text message submission to the active session
- opening HTTP and HTTPS URLs through Kit
- project, Git, session, turn-started, and turn-completed event notifications
- bidirectional request cancellation

The external v1 protocol intentionally omits:

- arbitrary runtime event subscriptions
- streaming tool progress
- direct theme access
- settings reads or updates
- current-model objects
- session mutation other than text message submission
- session transcript reads
- external debug sections
- raw runtime, shell, renderer, attachments, VCS controller, or keymap objects
- executable tool argument preparation callbacks

External plugins receive public normalized data transfer objects. Kit does not
serialize upstream Pi model or message objects directly.

## Trust model

This change does not introduce a sandbox or workspace approval prompt. User and
project plugins continue to be trusted local code and launch automatically
after discovery.

Plugin processes run as the same OS user as Kit, inherit Kit's environment, and
may access files, the network, and subprocesses available to that user. The
process and RPC boundary isolates Kit internals and plugin lifecycle; it does
not protect the user's machine from malicious plugin code.

Kit does not install dependencies for protocol plugins. The plugin author or
user is responsible for making the manifest command runnable.

## Migration

This is a clean replacement, not a compatibility period. The external
TypeScript loader, npm dependency installation, Bun plugin bundling,
`@akonwi/kit/plugin` runtime export, and bundled TypeBox plugin SDK are removed
as part of the implementation.

The release notes must include a migration guide covering:

- moving from a direct `*.ts` file to `<plugin>/plugin.json`
- replacing initializer calls with JSON-RPC registration requests
- replacing callbacks with command, tool, interceptor, and click request
  handlers keyed by local contribution id
- replacing `kit.on` subscriptions with documented event methods
- replacing `kit.logger.log` with stderr
- replacing TypeBox tool parameters with the Kit Tool Schema Profile v1
- replacing function disposers with explicit id-based removal where dynamic
  removal is needed

## Consequences

### Positive

- Plugins can be authored in any language that can run a process and exchange
  JSON messages over stdio.
- The external contract is independent from Bun, npm, TypeScript, and TypeBox.
- Plugin crashes and invalid protocol output are isolated from the Kit process.
- Capability ownership, cleanup, conflicts, and invocation become explicit
  wire behavior.
- A machine-readable protocol can drive runtime validation and future language
  SDKs.

### Trade-offs

- Kit must own a full-duplex JSON-RPC endpoint, process manager, schema
  validation, cancellation, backpressure, and failure reporting.
- Every callback becomes an asynchronous process round trip.
- Plugins must reserve stdout exclusively for the protocol and use stderr for
  logs.
- The protocol becomes a compatibility commitment that must evolve deliberately.
- Existing TypeScript plugins require migration rather than continuing to load
  unchanged.

## Related

- `docs/adrs/0015-plugin-system.md`
- `docs/adrs/0022-function-plugin-api.md`
- `docs/adrs/0023-keymap-driven-keybindings.md`
- `docs/adrs/0024-retarget-session-cwd.md`
- `docs/features/plugins.md`
- `docs/plugin-protocol/v1.md`
