# RPC and web modes

Kit can run its agent runtime without OpenTUI and expose commands, responses,
and streaming runtime events to another process or network client.

Two transports share the same session host and record model:

- `kit --mode rpc` uses newline-delimited JSON over stdio.
- `kit --mode web` uses one JSON record per WebSocket message.

This protocol controls Kit itself. It is separate from the external-plugin
JSON-RPC protocol, where Kit owns and controls plugin subprocesses.

## Record model

A command is an object with a `type` and an optional client-scoped `id`:

```json
{"id":"prompt-1","type":"prompt","message":"Review this change"}
```

Kit responds on the originating transport connection:

```json
{"id":"prompt-1","type":"response","command":"prompt","success":true}
```

A failed command has `success: false` and an `error` string. Runtime events do
not carry command ids and are published independently from responses.

Prompt acceptance is asynchronous. A successful prompt response means Kit
accepted the prompt; `agent_settled` marks the end of the resulting run.

## Commands

The current protocol version is 2. Send `get_capabilities` to discover the
commands supported by the running Kit host. For example, stdio RPC omits
`ui_response` because it does not install a remote interaction broker.

| Command | Important fields | Behavior |
| --- | --- | --- |
| `get_capabilities` | | Return protocol version and command names. |
| `prompt` | optional `message`, `attachmentIds`, `streamingBehavior` | Start a run with text and uploaded attachments, or steer/follow up with text while streaming. |
| `steer` | `message` | Inject steering into the active run. |
| `follow_up` | `message` | Queue a follow-up message. |
| `abort` | | Abort active work and wait for accepted runs to stop. |
| `new_session` | | Create and activate a session. |
| `list_sessions` | optional `cwd` | List lightweight session summaries. |
| `open_session` | `sessionId` | Open a persisted session by exact opaque id while idle. |
| `change_cwd` | `cwd` | Change the hosted workspace while idle. |
| `list_commands` | | List commands with transport-neutral handlers. |
| `execute_command` | `commandId`, optional `args` | Execute a listed transport-neutral command. |
| `ui_response` | `requestId`, `response` | Resolve a pending interaction when `interactiveUI` is enabled. |
| `get_state` | | Return model, thinking, streaming, session, cwd, and message counts. |
| `get_messages` | optional `offset`, `limit` | Return all active transcript messages, or a page of at most 200 with pagination metadata. |
| `get_message_chunk` | `token`, optional `offset`, `maxBytes` | Web-only: return up to 32 KiB of one browser-safe JSON message as base64. |
| `get_pending_interactions` | optional `offset`, `limit` | Page pending remote interaction requests when interactive UI is enabled. |
| `get_pending_interaction_chunk` | `requestId`, optional `offset`, `maxBytes` | Return up to 48 KiB of one pending request as base64 JSON. |
| `get_last_assistant_text` | | Return the last assistant message's text. |
| `get_available_models` | | Return authenticated models. |
| `set_model` | `provider`, `modelId` | Change model while idle. |
| `get_available_thinking_levels` | | Return thinking levels for the active model. |
| `set_thinking_level` | `level` | Change the thinking level. |
| `switch_session` | `sessionPath` | Legacy session-open operation retained for stdio compatibility. |

Commands are serialized by one authoritative session host. Different network
clients may reuse the same command id because response correlation is scoped to
the connection that sent the command. `ui_response` bypasses the mutation queue
so it can resolve an interaction requested by a currently executing command.
Only commands with an explicit transport-neutral execution handler are exposed
through `list_commands`; renderer-owned commands are not run with fabricated
TUI context. Remote commands execute only while the agent is idle, receive an
abort signal, and time out after 30 seconds. `abort` runs out of band, cancels
the active command, and invalidates commands that were already queued behind
it.

## Events

Kit currently publishes these event types:

- `agent_start`, `agent_end`, and `agent_settled`
- `turn_start` and `turn_end`
- `message_start`, `message_update`, and `message_end`
- `tool_execution_start`, `tool_execution_update`, and `tool_execution_end`
- `queue_update`
- `auto_retry_start` and `auto_retry_end`
- `state_changed`
- `ui_request` and `ui_resolved`
- `error`

`state_changed` carries a fresh state snapshot after shared state mutations such
as session, model, or thinking-level changes. Web mode broadcasts runtime events
to every connected client.

## Event sequencing and reconnection

Web events include one host-instance `streamId` and a monotonically increasing
`sequence`. The same shared event has the same sequence for every client;
connection-scoped command responses are not sequenced. Web capability responses
advertise `eventSequencing`, the current stream and sequence, and retention
limits. Stdio capabilities report sequencing as unsupported.

Every WebSocket connection begins with `sync` and ends synchronization with
`sync_complete` before live delivery starts. With no resume cursor, or when a
cursor is invalid, stale, or from another host instance, `sync` uses
`mode: "snapshot"` and includes current state, pending interactions, and a
bounded transcript tail. Snapshots retain at most the latest 200 messages and
64 KiB; `messageOffset`, `totalMessageCount`, and `messagesTruncated` identify
missing history, which clients retrieve through paginated `get_messages`
commands. Web message pages are also capped at 64 KiB. An individually oversized
message becomes a `message_reference` and is reconstructed through bounded
`get_message_chunk` responses. Reference tokens address immutable browser-safe
serialized bytes in a 16 MiB bounded cache, so transcript mutation cannot mix
chunk versions and chunks never restore projected image data or server paths.
Evicted tokens are rejected; a single projected message above the cache limit is
reported as `message_unavailable`.

A reconnecting client requests replay with:

```text
/api/rpc?streamId=<previous-stream-id>&after=<last-applied-sequence>
```

When the cursor remains in the journal, the server sends a replay `sync` whose
`sequence` is the requested starting cursor and whose `targetSequence` is the
captured high-water mark, followed by retained events and `sync_complete`.
Clients advance their durable cursor only as each event is applied; they must
not persist `targetSequence` before receiving the replayed tail. The server then
adds the connection to live broadcasts, so replay and live delivery cannot
interleave or leave a gap.

The journal retains a contiguous suffix of at most 2,048 projected events and
8 MiB. Oversized or evicted history causes snapshot fallback. If an internal
event cannot be serialized, clients receive `resync_required` and should
reconnect without a cursor to obtain a snapshot.

## Remote interactions

Web-mode capability responses set `interactiveUI` to true and list supported
`interactionKinds`. The server emits a request such as:

```json
{
  "type": "ui_request",
  "request": {
    "id": "request-id",
    "kind": "confirm",
    "createdAt": "2026-01-01T00:00:00.000Z",
    "payload": { "title": "Proceed?", "message": "Run the command" }
  }
}
```

A client answers with a normal correlated command:

```json
{
  "id": "command-id",
  "type": "ui_response",
  "requestId": "request-id",
  "response": { "confirmed": true }
}
```

Supported response bodies are:

| Interaction kind | Response |
| --- | --- |
| `confirm` | `{ "confirmed": boolean }` |
| `input` | `{ "value": string \| null }` |
| `select` | `{ "optionId": string \| null }` |
| `guided_questions` | `{ "cancelled": boolean, "answers": object }` |

Select option ids are opaque transport values; the server maps them back to the
in-process values owned by the requesting plugin. Guided-question answers are
validated against their declared kinds, required fields, and options.

Snapshot and listing records replace an interaction payload over 16 KiB with a
reference. Clients reconstruct it through `get_pending_interaction_chunk`; they
can page omitted interaction listings through `get_pending_interactions`.

Requests are broadcast to every connected client. The first valid response
wins, and `ui_resolved` tells all clients to dismiss the interaction. Invalid,
duplicate, and late responses fail without resolving another request. Pending
requests are independent of client connections: they are replayed to every
client that connects and remain pending until any client answers, the
originating operation aborts, or the server shuts down.

## Attachments

Web mode accepts one multipart `file` field at `POST /api/attachments` and
returns `{ "attachment": { "id", "filename", "mimeType", "size", "kind",
"createdAt" } }`. A subsequent idle `prompt` command references up to eight
opaque ids through `attachmentIds`; `message` may be empty when at least one
attachment is present. Read an available or accepted upload with
`GET /api/attachments/<id>`, and delete an unused or no-longer-needed retained
upload with `DELETE /api/attachments/<id>`. Text downloads use passive
attachment semantics and MIME sniffing is disabled.

Non-interlaced PNG, baseline JPEG, bounded GIF, and single-image WebP uploads
become image message parts after structural and dimension validation. Other
uploads must be UTF-8 text and become delimited prompt text. Images are limited
to 10 MiB each, text files to 1 MiB each, and one prompt to 20 MiB total with at
most 1 MiB of text files. The host enforces a 50 MiB staged-and-accepted
attachment budget with a 64 KiB minimum charge per accepted attachment;
deleting an unsubmitted upload releases its charge, while accepted history
remains charged until the host stops. Uploads are one-shot once the runtime
accepts the user message; failed pre-acceptance submission releases the ids for
retry.

Accepted remote images retain their attachment id until the client deletes its
download copy or the web host stops; deleting the copy does not release the
accepted-history budget. WebSocket records omit inline image base64 and report
`dataOmitted: true` with `attachmentId` and image metadata instead; clients
render the image through the authenticated/trusted HTTP boundary. This keeps
uploads and persisted image messages from exceeding WebSocket backpressure
limits.

## Stdio transport

Run:

```sh
kit --mode rpc
kit --mode rpc --session <id>
kit --mode rpc --model <provider>/<model-id>
kit --mode rpc --no-session
```

stdin carries UTF-8 JSONL commands, stdout carries responses and events, and
stderr carries diagnostics. Readers accept CRLF and a final unterminated JSON
record. Malformed and unknown commands produce failure responses without
terminating the process.

Closing stdin causes Kit to stop accepting work, abort active work, flush
responses, persist when enabled, and exit.

## WebSocket transport

Run:

```sh
kit --mode web
kit --mode web --session <id>
kit --mode web --host 0.0.0.0 \
  --allow-host machine.example:4782 \
  --allow-origin https://machine.example
```

Web mode defaults to `127.0.0.1:4782` and serves:

```text
GET /                 browser entry point
GET  /api/health              process health and connected-client count
POST /api/attachments         multipart attachment upload
GET  /api/attachments/<id>    attachment content
DELETE /api/attachments/<id>  release attachment content
WS   /api/rpc                 commands, responses, events, and synchronization
```

Multiple WebSocket clients may control the same runtime. Command responses go
only to their originating connection; runtime events are broadcast to all
connections.

WebSocket upgrades require an allowed request host and an allowed complete
`Origin`. Loopback hosts and their same origins are allowed automatically.
Non-loopback deployments use one or more `--allow-host <host:port>` options.
Reverse proxies that change the request origin also use an explicit allowed
origin. Deployments should remain behind a trusted private boundary such as
Tailscale or SSH. Native Kit authentication is not part of the initial web
mode.

The server limits inbound messages to 1 MiB and buffered per-client output to
16 MiB. A client that exceeds the backpressure limit is disconnected rather
than being allowed to consume unbounded memory or silently lose protocol
ordering. Web mode omits the complete message-history copy from `agent_end`;
clients build the transcript from message events and request snapshots when
needed.

## Current web-mode limitations

The initial web transport establishes the shared host, HTTP lifecycle,
multi-client WebSocket behavior, remote interactions, external-plugin loading,
and transport-neutral command/session operations. The browser route is
currently a placeholder.
Before web mode reaches feature parity it still needs:

- the Mica-based transcript and composer client
- transport-neutral adapters for additional renderer-owned built-in commands

See ADR 0027 and `backlog/remote-session-server.md` for the intended design and
delivery sequence.

## Related

- `docs/adrs/0025-json-rpc-plugin-protocol.md`
- `docs/adrs/0026-headless-rpc-mode.md`
- `docs/adrs/0027-remote-session-server.md`
- `backlog/remote-session-server.md`
