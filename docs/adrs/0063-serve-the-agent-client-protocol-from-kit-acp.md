# 0063: Serve the Agent Client Protocol from `kit acp`

## Status

Accepted. Realizes the stdio protocol client of
[ADR 0001](0001-native-go-architecture.md) and the stdio RPC mode deferred by
[ADR 0008](0008-establish-initial-native-cli-surface.md) as an Agent Client
Protocol agent instead of a Kit-owned schema. Follows
[ADR 0027](0027-treat-print-as-an-ordinary-session-client.md): `kit acp` is an
ordinary session client.

## Context

The [Agent Client Protocol](https://agentclientprotocol.com) (ACP) is an open
protocol between coding agents and the programs that present them, such as
editors. The client runs the agent as a child process and they exchange
JSON-RPC 2.0 messages, one per line on stdin and stdout, with protocol version
`1` negotiated by `initialize`. The client owns the conversation view, file
navigation, and permission prompts; the agent owns the model, tools, and
session state. Optional behavior on both sides is negotiated through declared
capabilities. A Kit-owned stdio schema would serve only clients written for it,
while any ACP client, Zed among them, can use any agent that conforms.

Kit's Go server is authoritative for sessions. Clients reach it through the
session API (the contract in `api/contract`, the public Go client in `api`,
and the ordered `SessionEvent` stream of
[ADR 0011](0011-use-sse-for-session-events.md) and
[ADR 0032](0032-model-protocol-variants-as-discriminated-unions.md)).
`kit print` is a session client that discovers or starts the local server,
runs one turn, keeps stdout for results, and writes diagnostics to stderr.

A stdio protocol process has stricter obligations than `kit print`. Its stdout
carries only protocol records; any stray byte corrupts the stream. It must
answer malformed input and keep reading. It must accept a prompt without
blocking the input loop, because cancellation and replies to its own requests
arrive while the prompt runs, and it must settle every prompt it accepts.

## Decision

`kit acp` is a headless command that speaks ACP version 1 as an agent on stdin
and stdout. It is a session API client: it imports the public Go client and
contract, never the droids runtime, SQLite stores, or server internals, and it
adds no session API operations.

### Conformance

The target is the stable ACP v1 specification, not a particular client. `kit
acp` meets every requirement the specification places on agents, with the one
exception named under MCP servers, and it relies on nothing outside the
specification:

- It uses an optional client method or update only when the client advertised
  the matching capability, and behaves correctly for every combination of
  client capabilities.
- It uses no client-specific extensions, `_meta` conventions, or unstable
  methods.
- Its records are checked against the official v1 JSON schema, and it is run
  against the ACP Test Compatibility Kit for v1.

Zed is the first client it is checked with by hand; other clients are expected
to work without changes to Kit.

### Process and server

`kit acp` resolves the Kit home, discovers or starts the local server, and
connects as `kit print` does. One process serves one ACP connection and any
number of ACP sessions on it. Each ACP session is attached to one Kit session
through the public client and watched through its event stream. When stdin
closes, `kit acp` detaches from its sessions without aborting their turns and
exits 0.

`kit acp` connects only to the local server in v1. Clients send local absolute
paths as the session cwd, and a saved remote server
([ADR 0045](0045-connect-clients-to-local-ssh-and-network-servers.md)) does not
share that filesystem.

### Protocol layer

Kit implements the JSON-RPC framing and the ACP v1 records it uses in a small
package in `apps/cli`, rather than depending on a third-party SDK. The
maintained Go SDK (`github.com/coder/acp-go-sdk`, Apache-2.0) trails the
current schema and drops malformed lines without answering them, which this
decision requires. The package defines only the methods and fields Kit sends
or reads; unknown fields from the client are ignored, as ACP's extensibility
rules require.

### Initialization and capabilities

`initialize` answers with `protocolVersion: 1` whatever version the client
requests; a client that cannot speak version 1 closes the connection. Kit
reports `agentInfo` with its name, title, and version, and advertises:

- `loadSession: true`;
- `sessionCapabilities`: `list`, `resume`, and `close`;
- `promptCapabilities`: `image` and `embeddedContext`, not `audio`;
- `mcpCapabilities`: neither `http` nor `sse`;
- `authMethods`: none.

Kit sends `compaction_update` only to a client that advertised
`session.compaction`, `notice` only to one that advertised `session.notices`,
and `elicitation/create` only in a mode the client advertised. Without them,
the information goes to stderr, and interactions are handled as described
under Interactions. Kit's config options are `select` options, which need no
client capability. It does not use the client's `fs/*` or `terminal/*`
methods.

Requests other than `initialize` that arrive first are answered with `-32600`.

### Sessions

The ACP session ID is the Kit session ID.

- `session/new` creates a persisted Kit session at the request's `cwd` with the
  server's default model and thinking level, attaches to it, and returns its
  config options. `kit acp --temp` creates temporary sessions instead, disposed
  by `session/close` or when the process exits.
- `session/list` lists persisted Kit sessions, filtered by `cwd` when given, in
  the server's order, with the session name as `title` and `updatedAt`. A
  session created by the TUI or the macOS app is listed like one created over
  ACP. Pagination uses an opaque cursor.
- `session/load` attaches to an existing session, replays its transcript as
  `session/update` notifications, and then answers. `session/resume` attaches
  without replay.
- `session/close` cancels the connection's running prompt, as
  `session/cancel` does, and then detaches; a turn another client started
  keeps running. Closing an unknown session succeeds.
- A session-scoped request naming a session that is not open on the connection
  is answered with `-32002` and `data.sessionId`.
- `session/delete` is not offered; deleting a shared Kit session from an
  ACP client's history list is too easy to do by accident.

A request's `cwd` for `session/load` or `session/resume` must match the Kit
session's cwd; a mismatch is answered with `-32602`.

### Prompts

`session/prompt` is accepted asynchronously. Kit converts the prompt's content
blocks into a Kit prompt input, starts a turn, and answers the request when
that turn ends, after every update the turn produced has been written.

| ACP content block | Kit prompt input |
| --- | --- |
| `text` | prompt text |
| `resource_link` with a `file://` URI | the absolute path, in the prompt text, for the model to read with its tools |
| `resource_link` with another URI | the URI and name in the prompt text |
| `resource` with `text` | a fenced block in the prompt text, labelled with its URI |
| `resource` with `blob` | an uploaded session attachment |
| `image` | an uploaded session attachment when the session's model accepts images, and otherwise a note in the prompt text that the image was omitted |

A prompt whose text begins with `/NAME` naming one of the session's prompt
commands runs that command with the rest of the line as its arguments.
`/compact` compacts the session's context.

If the session already has a running turn, as when another client prompted it,
the prompt is answered with an error instead of joining the follow-up queue.

The prompt response's `stopReason` follows the turn:

| Kit turn outcome | ACP response |
| --- | --- |
| `completed` | `stopReason: "end_turn"` |
| `aborted` or `interrupted` | `stopReason: "cancelled"` |
| `failed` with a provider authentication error | error `-32000` |
| `failed` otherwise | error with the turn's error message |

### Session updates

Kit translates the session's event stream into `session/update`
notifications for every turn on an attached session, including turns another
client started, so the ACP client shows the same conversation as Kit's other
clients.

| Kit `SessionEvent` | ACP `session/update` |
| --- | --- |
| `user.message.added` for a turn this connection did not start | `user_message_chunk` |
| `plugin.message.added` | `user_message_chunk` |
| `assistant.text.delta` | `agent_message_chunk`, with the Kit message ID as `messageId` |
| `assistant.thinking.delta` | `agent_thought_chunk` |
| `tool.planned` | `tool_call`: `status: "pending"`, `title` from Kit's tool presentation, `name`, `kind`, `rawInput`, and `locations` from path arguments |
| `tool.started` | `tool_call_update`: `status: "in_progress"` |
| `tool.output.delta` | `tool_call_update` replacing `content` with the output so far, sent at most every 100 ms |
| `tool.completed` | `tool_call_update`: `status: "completed"` or `"failed"`, `content`, `rawOutput` |
| `compaction.started` | `compaction_update` `in_progress`, if the client supports compaction |
| `compaction.completed` | `compaction_update` `completed` or `failed`, if the client supports compaction |
| `provider.retry.scheduled` | `notice` with `severity: "warning"`, if the client supports notices |
| `context.changed`, `usage.changed` | `usage_update` with context `used`, `size`, and the session's cost in USD |
| `session.name.changed` | `session_info_update` with `title` |
| `session.configuration.changed` | `config_option_update` |
| `interaction.requested` | a permission or elicitation request, below |
| `interaction.resolved` by another client | `$/cancel_request` for the outstanding request |
| `turn.completed` | settles the ACP prompt that started the turn |

Tool kinds: `read` → `read`; `edit`, `write` → `edit`; `bash` → `execute`;
`grep`, `find`, `ls` → `search`; web fetching tools → `fetch`; everything
else, including MCP and subagent tools, → `other`. A completed `edit` adds a
`diff` content item for each replaced section, and a completed `write` a
`diff` with no `oldText`, so the client can show the change; other tools'
result text is a `content` item.

Other events, such as scratchpad, annotation, subagent, peer query, URL open,
and client plugin events, have no ACP form and are not sent. Kit sends no
`plan` updates: the scratchpad is free-form Markdown, not a task list.

`session/load` replays the transcript as the same updates: user messages as
`user_message_chunk`, assistant text and thinking as message and thought
chunks, and each tool call as a `tool_call` already in its final status.

### Interactions and permissions

Kit tools run without approval, so `session/request_permission` is used only
for interactions the model or a plugin requests:

- A `confirm` interaction becomes `session/request_permission` on its tool
  call, with an `allow_once` option labelled with the confirm label and a
  `reject_once` option labelled with the cancel label.
- `input`, `select`, and `guided` interactions, and a plugin's `confirm`,
  `input`, and `select`, become `elicitation/create` in form mode when the
  client supports it: a string, an enum, a boolean, or an array of enums per
  question. Accepting answers the interaction; declining or cancelling
  cancels it.
- An interaction the client cannot present, including `authorize`, is left
  pending for another attached client to answer, as ADR 0027 requires, and Kit
  writes a diagnostic to stderr.

`session/cancel` aborts the turn the connection's prompt started, which
cancels its pending interactions; a turn another client started is not
aborted. Kit then answers the prompt with `stopReason: "cancelled"`,
never with an error, after the turn's last update. `$/cancel_request` naming
an in-flight `session/prompt` does the same; naming another in-flight request
it answers that request with `-32800`, and naming no in-flight request it is
ignored.

### Configuration and commands

Kit exposes the session's model and thinking level as ACP config options: a
`model` option of category `model` listing the available models of the
server's catalog, and a `thinking` option of category `thought_level` listing
the current model's thinking levels, or only `off` for a model without them,
so the set of options does not change with the model.
`session/set_config_option` configures the session and answers with every
option. Kit offers no session modes.

After a session is opened, and when its prompt commands change, Kit sends
`available_commands_update` with the session's prompt commands, each with its
argument hint, and `compact`.

### MCP servers

Kit uses the MCP servers configured in Kit. Kit accepts the servers a client
passes in `session/new`, `session/load`, or `session/resume` but does not
connect to them; it writes a diagnostic to stderr and, when the client
supports notices, sends a `notice` naming them. This falls short of the
specification's requirement that agents support stdio MCP servers. Meeting it
needs the session API to accept session-scoped MCP servers, a separate
decision.

### Authentication

Provider credentials belong to the server and are managed with `kit auth` and
the TUI's `/login`. `kit acp` advertises no authentication methods. When no
provider is logged in, `session/new` is answered with `-32000` and a message
that says how to log in.

### Errors and stream hygiene

- `kit acp` writes protocol records to stdout through one writer and nothing
  else. At startup it moves the original stdout to a private descriptor and
  points descriptor 1 at stderr, so output from any other code reaches stderr.
- Logs and diagnostics go to stderr.
- A line that is not JSON is answered with `-32700` and `id: null`. A JSON
  value that is not a request, response, or notification is answered with
  `-32600`. A batch is answered with `-32600`. A line longer than 16 MiB is
  discarded up to its newline and answered with `-32700`. The connection keeps
  reading after each.
- A request for an unknown method, including an `_`-prefixed extension method,
  is answered with `-32601`; an unknown notification is ignored. Invalid
  parameters are answered with `-32602`.
- A notification never receives a response, even when it is malformed.
- A failure of the server or the session API is answered with `-32603` and
  its message.
- When the server cannot be reached at startup, `kit acp` still answers
  `initialize`, and answers session requests with `-32603` describing the
  failure, so the client can show it.

## Consequences

- Any conforming ACP client, Zed among them, can use Kit's sessions, models,
  tools, plugins, skills, and prompt commands, and a client's thread is the
  same Kit session the TUI and macOS app show.
- Clients see Kit's file changes on disk, not as edits to unsaved buffers, and
  commands run in Kit's tool environment, not in the client's terminal.
- MCP servers configured in a client are not available to Kit until a later
  decision adds session-scoped MCP servers to the session API.
- Turns another client is running cannot be joined from an ACP client.
- An ACP client's user must log in to a provider outside the client before
  first use.
- Kit must follow changes to ACP v1, checked by the schema and the TCK, and
  needs a new decision to adopt protocol version 2.

### Out of scope for v1

- the client's `fs/*` and `terminal/*` methods;
- connecting to MCP servers from the client;
- `session/delete`, session modes, and `plan` updates;
- `authorize` interactions and ACP authentication methods;
- connecting to saved remote servers;
- ACP protocol version 2 and unstable ACP methods.

## Related decisions

- [0001: Native Go application architecture](0001-native-go-architecture.md)
- [0008: Establish the initial native CLI surface](0008-establish-initial-native-cli-surface.md)
- [0011: Use SSE for session event delivery](0011-use-sse-for-session-events.md)
- [0012: Route model-user interactions through the session](0012-model-user-interactions.md)
- [0027: Treat print as an ordinary session client](0027-treat-print-as-an-ordinary-session-client.md)
- [0045: Connect clients to local, SSH, and network servers](0045-connect-clients-to-local-ssh-and-network-servers.md)
