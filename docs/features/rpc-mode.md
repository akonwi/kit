# RPC and web modes

Kit's stdio and WebSocket transports share the same `RpcSessionHost`, command
dispatch, and semantic event model.

Kit can run as a long-lived headless subprocess using newline-delimited JSON on
stdin and stdout:

```bash
kit --rpc
kit --rpc --no-session
kit --rpc --session <id-or-path>
kit --rpc --model <provider>/<model-id>
kit --web --no-session
```

RPC mode follows newline-delimited command and response envelope conventions
similar to Pi's RPC mode, but its event schema is Kit-owned. Provider and Pi
streaming types do not cross the protocol boundary.

## Remote terminal clients

A future `kit attach` command will run Kit's OpenTUI shell locally while using
the web-mode protocol to control an authoritative remote `kit --web` session.
It is not implemented yet. The terminal client will reconstruct presentation
state from snapshots and sequenced semantic events rather than start another
runtime or consume terminal-byte output.

`kit --web-tui` is a separate experimental mode: it runs the renderer on the
server and streams terminal bytes to a browser. A future OpenTUI SSH entry point
would have the same server-rendered shape. Neither is the protocol foundation
for `kit attach`.

## Transport and lifecycle

- Write one JSON command per line to stdin. LF and CRLF are accepted, as is a
  final record without a trailing newline.
- Read one JSON response or event per line from stdout.
- stdout is reserved for protocol records. Kit logs and diagnostics go to
  stderr.
- Responses and events can interleave while accepted prompts execute. Include
  a string `id` to correlate a response with its command.
- A successful `prompt` response means the prompt was accepted, not that the
  model has completed. Wait for `agent.settled` and inspect
  `agent.message.ended` or call `get_last_assistant_text` for the final result.
- Closing stdin shuts down Kit. SIGINT and SIGTERM abort active work and exit
  with status 130 and 143, respectively.

RPC and web modes resume the most recent Kit session for the current directory
by default, creating and persisting one when none exists. `--session` opens an
existing session. `--no-session` keeps the main conversation, scratchpad, and
sub-agent conversations in memory.

An existing session restores its saved model when that model is available.
`--model` overrides it at startup using an exact `<provider>/<model-id>` pair:

```bash
kit --rpc --model openai/gpt-5.5
kit --rpc --model openrouter/openai/gpt-5.5
```

Everything after the first slash is the model ID, so model IDs that contain
slashes are supported. Only authenticated, selectable models can be chosen.
Controllers can discover those exact IDs through the protocol:

```json
{"id":"models","type":"get_available_models"}
{"id":"model","type":"set_model","provider":"<provider>","modelId":"<model-id>"}
```

Like print mode, RPC mode loads headless-safe built-ins plus user and project
external plugins discovered from `~/.kit/plugins/` and the active session cwd.
Headless-compatible contributions such as tools, tool-call interceptors,
subagents, system-prompt slots, and lifecycle events are active. Commands are
remotely executable only when they provide an explicit transport-neutral
handler. Stdio RPC has no presentation for chrome or interactive plugin UI, so
those interactions fail or return cancellation. Web mode projects supported
plugin chrome and routes confirmation, input, selection, and guided-question
requests through the remote interaction protocol.

## Web mode authentication

Web mode can protect every browser document, asset, API, attachment, health
check, and WebSocket upgrade with HTTP Basic authentication:

```sh
kit --web --auth 'username:password'
```

Kit splits the value on the first colon. The username must be non-empty and
cannot contain a colon; the non-empty password may contain additional colons.
The browser presents its native Basic authentication prompt and reuses the
credentials for same-origin requests and WebSocket reconnects.

Basic authentication credentials are scoped to the running Kit process and are
not written to persisted sessions. Because Basic authentication sends the
credential with every request, remote use requires an HTTPS tunnel or reverse
proxy; Kit's web server does not terminate TLS. Command-line arguments may also
be visible in shell history and local process inspection. Host and Origin
validation remains active when authentication is enabled.

## Web mode network validation

Kit binds to `127.0.0.1` by default and permits the bound address, localhost
aliases, and each request's same origin. A hosted deployment can declare one
canonical external HTTP(S) origin:

```sh
kit --web \
  --public-url https://kit.example.com \
  --auth 'username:password'
```

`--public-url` must be an origin without credentials, a subpath, query, or
fragment. It adds the external hostname and browser Origin to Kit's validation
and adds the corresponding `ws://` or `wss://` source to the document CSP. It
does not change the listener; combine it with `--host` when the process must
bind another interface. Kit does not implicitly trust `Forwarded` or
`X-Forwarded-*` headers.

Additional reverse-proxy or tunnel addresses can still be added without losing
local browser access:

```sh
kit --web \
  --allow-host kit.example.internal \
  --allow-origin https://kit.example.internal
```

Repeat either option to add more values. A host value includes its port when the
browser sends one; an origin includes its scheme and optional port.

Deployments whose surrounding network or proxy owns these checks can explicitly
disable either allowlist:

```sh
kit --web --allow-host '*' --allow-origin '*'
```

These wildcards are opt-in rather than defaults. `--allow-host '*'` accepts any
request Host header. `--allow-origin '*'` accepts any valid Origin value,
including the opaque `null` origin, for protected HTTP mutations and WebSocket
upgrades; originless requests remain rejected where they were previously
required to carry an Origin. Kit warns when a wildcard is used without
`--auth`. Even with authentication, only use an origin wildcard behind HTTPS
and a trusted network or access-control proxy, because it disables Kit's
cross-site request protection.

### Hosted deployment boundary

Kit's web server deliberately does not infer whether a listener is publicly
reachable. An explicit `--host` controls binding and loopback remains the safe
default. The hosting environment owns TLS termination, user identity, network
ACLs, ingress exposure, rate limiting, resource limits, and egress policy.
Kit retains application-level Origin validation because browsers can
implicitly send proxy cookies or HTTP credentials during a cross-site
WebSocket attempt; Host validation remains inexpensive defense-in-depth.

A hosted Kit process is single-principal. It can execute commands and access its
process user's workspace, credentials, tools, and session storage. Multi-user
deployments must isolate principals into separate processes or containers with
scoped workspaces and homes. Kit's optional Basic Auth is not a tenancy or
sandbox boundary and is confidential only when the browser connects over
HTTPS.

### HTTP and WebSocket boundary

Web mode defaults to `127.0.0.1:4782` and serves:

```text
GET    /                       browser entry point
GET    /api/health             process health and connected-client count
POST   /api/attachments        multipart attachment upload
GET    /api/attachments/<id>   attachment content
DELETE /api/attachments/<id>   release attachment content
WS     /api/rpc                commands, responses, events, and synchronization
```

The server limits inbound WebSocket messages to 1 MiB and buffered output per
client to 16 MiB. A client that exceeds the backpressure limit is disconnected
rather than being allowed to consume unbounded memory or lose protocol ordering.

### Mobile browser layout

At phone widths, web mode keeps the transcript and composer as the primary
surfaces while adapting desktop chrome for touch:

- Model, thinking, scratchpad, and remote header contributions move into the
  session menu.
- The command palette has a dedicated composer action rather than requiring a
  hardware-keyboard shortcut.
- Workspace activity and scratchpad tabs retain a shared, always-available
  composer beneath either tab.
- The desktop footer is hidden. Connection failures, synchronization, errors,
  and queued-message state appear in a temporary strip above the composer;
  location and footer contributions move into the session menu.
- The command palette becomes a finger-scrollable Mica bottom sheet and opens
  without a filter keyboard. Commands that require arguments reveal the input
  only after selection.
- Empty pending state does not reserve space. The compact composer uses labeled
  icon buttons, smaller mobile typography, and coarse-pointer Enter inserts a
  newline instead of submitting.
- The shell tracks the visual viewport and safe-area insets so the software
  keyboard does not cover the composer.

Desktop layout and keyboard behavior remain unchanged.

## Responses

Success:

```json
{"id":"1","type":"response","command":"get_state","success":true,"data":{}}
```

Failure:

```json
{"id":"1","type":"response","command":"unknown","success":false,"error":"Unknown command: unknown"}
```

Malformed records produce a failed `parse` response and do not stop the
process.

## Commands

The current protocol version is 2. Clients should use `get_capabilities` to
confirm the host version, supported commands, features, and transport limits.

| Command | Fields | Result data |
| --- | --- | --- |
| `prompt` | optional `message`, `attachmentIds`, `streamingBehavior: "steer" \| "followUp"` | none; accepted asynchronously |
| `steer` | `message` | none |
| `follow_up` | `message` | none |
| `restore_follow_ups` | `clientId`, `operationId`, `sessionId`, `expectedGeneration` | ordered text-only `messages` plus the resulting queue `generation` |
| `promote_follow_ups` | `sessionId`, `expectedGeneration` | promoted `count` plus the resulting queue `generation`; active runs only |
| `acknowledge_follow_up_mutation` | `clientId`, `operationId` | release a retained non-empty restore after applying it safely |
| `abort` | none | none |
| `new_session` | none | `{ "cancelled": false }` |
| `list_sessions` | optional `cwd` | lightweight persisted-session summaries |
| `open_session` | `sessionId` | activate an exact opaque session while idle |
| `change_cwd` | `cwd` | change the hosted workspace while idle |
| `get_capabilities` | none | protocol version, features, and transport limits |
| `get_state` | none | current model, thinking level, session, CWD, streaming, message counts, queued follow-up previews, and context usage |
| `get_messages` | optional `offset`, `limit` | transcript messages with pagination metadata |
| `get_message_chunk` (web mode) | `token`, optional `offset`, `maxBytes` | one bounded chunk of an oversized projected message |
| `get_pending_interactions` (web mode) | optional `offset`, `limit`, `generation` | a generation-guarded page of pending remote interactions |
| `get_pending_interaction_chunk` (web mode) | `requestId`, optional `offset`, `maxBytes` | one bounded chunk of an oversized interaction |
| `ui_response` (web mode) | `requestId`, `response` | resolve a pending remote interaction |
| `get_last_assistant_text` | none | `{ "text": string \| null }` |
| `get_available_models` | none | `{ "models": [...] }` |
| `set_model` | `provider`, `modelId` | selected model |
| `get_available_thinking_levels` | none | `{ "levels": [...] }` |
| `set_thinking_level` | `level` | none |
| `get_scratchpad` | none | active `sessionId` and Markdown `content` |
| `update_scratchpad` | `sessionId`, `expectedContent`, `content` | saved `sessionId` and `content` |
| `get_review_state` (web mode) | none | active session and review generation plus changed-file summaries |
| `get_review_file` (web mode) | `path` | one changed file with its patch and hunks |
| `submit_review` (web mode) | `submissionId`, `sessionId`, `generation`, non-empty `notes` with `path`, side, line range, and comment | none; accepted asynchronously as a code-review message |
| `switch_session` | `sessionPath` (a Kit session ID or path) | `{ "cancelled": false }` |
| `activate_chrome_contribution` (web mode) | `area: "header" \| "footer"`, `contributionId` | none |
| `list_commands` | none | transport-neutral commands plus `registryGeneration` |
| `execute_command` | `commandId`, optional `args`, optional `registryGeneration`, optional `expectedSessionId` | none |

Clients that present commands interactively should pass the generation returned
by `list_commands` to `execute_command`. Kit rejects stale generations when the
command registry changes, preventing a displayed command from resolving to a
newly registered implementation with the same ID. Generations are scoped to one
RPC host incarnation, so clients must discard them after reconnecting to a new
event stream. The generation remains optional for compatibility with
controllers that execute a known command ID directly. Session-specific browser
adapters can also pass `expectedSessionId`; Kit rejects the command if another
client changes the active session before execution.

### Remote command exposure

Remote command execution is explicit rather than inferred from ordinary command
registration. A transport-neutral handler must not depend on renderer-owned
pickers, workspaces, or local process controls, and must propagate failures
through RPC instead of converting them only into TUI toasts.

The exposed built-in transport-neutral batch is `/compact`, `/handoff`, `/name`
(with an explicit name), `/new`, `/reload`, and `/cd` (with an explicit path).
Discovered
prompt-template commands and Claude-compatible `/cc:<name>` commands are also
exposed when their files are available. Transport handlers receive host-provided
runtime, persistence, cancellation, and prompt scheduling context rather than
capturing one session inside the static built-in command definitions. Internal
plugin commands must also opt in individually; they are not remotely executable
by default. When multiple registrations use the same command ID, the first
registration owns that ID for both remote listing and execution.

`/new` and `/handoff` follow the host's persistent or ephemeral session policy.
When `/handoff` includes a message, command success acknowledges the completed
fork and accepts the message as an asynchronous prompt; normal agent lifecycle
events report its progress and settlement. Claude-compatible commands use the
same asynchronous acceptance boundary while preserving their structured
prompt-command name, arguments, and expanded prompt in the transcript. Dynamic
prompt and Claude command registrations refresh when the session cwd changes;
Claude command discovery also rejects workspace-escaping paths.

Browser-local `/model` and `/thinking` palette commands use the existing
discovery and mutation RPC commands through reusable browser-native pickers.
Browser-local `/theme` offers system, light, and dark appearances without
requiring theme tokens over RPC, and persists the choice in browser storage.
Browser-local `/toggle-scratchpad` opens a responsive Markdown workspace panel;
its guarded autosaves update the session sidecar through RPC and live change
events keep clean drafts synchronized with agent and other-client edits. The
browser code-review surface loads changed files incrementally and submits local
line notes as a structured code-review message. Submission is guarded by both
the active session and review generation; the host reloads the working-tree diff
and rejects stale paths or line ranges instead of silently rebinding comments.
Client-generated submission IDs are persisted with the review message, making
response-loss retries idempotent while that accepted message remains in current
session history, including across host restarts.
The clickable session title similarly adapts the transport-neutral `/name`
command through a browser-native input dialog. The transport-neutral `/reload`
command reloads the active session, settings, and plugin state in the shared
host, matching its TUI behavior. A disconnected browser instead shows a
`Reconnect` button for an immediate connection attempt. Sessions,
authentication, diagnostics, sub-agents, MCP management, settings, and release
notes require purpose-built remote surfaces. The MCP surface will
include clearing a server's saved OAuth state rather than exposing
`/mcp-logout` as a remote slash command. Imperative plugin `kit.system.open`
calls enqueue a correlated, nonblocking `open_url` browser action; the pending
action queue is bounded and deduplicated, and only HTTP and HTTPS URLs are
accepted remotely. `/quit` remains
unavailable in the browser; `/pager` remains renderer-local.

A normal `prompt` is rejected while the agent is streaming unless
`streamingBehavior` says where to queue it. The dedicated `steer` and
`follow_up` commands provide the same queue controls directly. `set_model`,
`new_session`, and `switch_session` are also rejected while the agent is
streaming; successful responses wait for any model-adaptation compaction to
finish before the next command is dispatched.

## Events

RPC mode emits Kit semantic events using the same dotted names as the runtime.
`agent.end` does not contain a complete message-history copy; clients reconstruct
the transcript from identified message events and request snapshot or paginated
state when recovery is required.

- `agent.start`, `agent.end`, and `agent.settled`
- `agent.turn.started` and `agent.turn.completed`
- `user.message.created`
- `agent.message.started`, `agent.message.updated`, and
  `agent.message.ended`
- `agent.tool.started`, `agent.tool.updated`, and `agent.tool.ended`
- `session.message.appended` and `session.handoff_summary.appended`
- `session.transcript.replaced` when recovery or compaction invalidates the
  current transcript projection
- bounded `session.compaction.completed.*` and `session.compaction.failed.*`
  outcome records for user feedback; completed adaptation compaction remains
  represented only by transcript replacement
- `chat.message-queue.changed` and `chat.followups.promoted`
- `agent.retry.started`, `agent.retry.failed`, and `agent.retry.completed`
- `agent.run.failed` for an execution failure after a command was accepted
- `scratchpad.changed` when the active session scratchpad changes
- `shell.chrome.changed` when web-mode plugin header/footer contributions,
  declarative URL actions, or built-in visibility claims change

Connection snapshots, `get_state` responses, and `state_changed` events include
`contextUsage` as `{ tokens, contextWindow, percent }` when a model provides a
context window, or `null` otherwise. Updated usage is published after completed
turns, compaction, model changes, and session changes so remote clients can
render the same threshold-colored progress indicator as the TUI.

State also includes `pendingMessageCount`, a server-owned
`pendingMessageGeneration`, and up to three normalized, length-bounded
`pendingMessagePreviews`. Live `chat.message-queue.changed` events add the same
`generation`, total `count`, and bounded `previews`; the existing `steering` and
`followUp` fields remain for protocol-v2 compatibility. New remote clients use
the bounded fields to render a compact queue.

Restoring or promoting queued follow-ups requires the active `sessionId` and the
observed generation. Commands are serialized, so the first client with a current
pair claims the entire observed queue. Any later command using that generation
fails without mutation. Restore returns the queued text messages in order for
the winning client to merge into its local draft; promote preserves their order
while moving them to steering. Structured follow-ups and restores above the
advertised `queuedFollowUps.maxDraftBytes` or `maxDraftItems` limit fail before
draining the queue. Restore requires stable client and operation IDs; retrying
the same operation from that client after response loss returns its retained
result without mutating twice. Clients acknowledge non-empty restores only after
applying them safely. The host rejects new retained restores at
`maxPendingMutations` rather than evicting an unacknowledged result. Empty
restores and promotions do not consume claim capacity. The browser keeps the
compact queue preview read-only and exposes batch **Edit in composer** and
**Send now** actions. Restored messages are placed before any existing draft;
the latter action promotes the queue to steering. Composer drafts are stored per session in browser storage so acknowledged
restores survive reloads and tab closure.

Every transcript message has a stable `turnId` and `messageId`.
`agent.message.updated.update` is a Kit-owned content update with a `kind`,
`contentType`, and `contentIndex`; content deltas also include `delta`.
`agent.message.ended.message` is the authoritative completed assistant message.
`session.message.appended.message` is the authoritative committed transcript
message and also covers non-assistant messages such as tool results.
`session.transcript.replaced` requires clients to obtain a fresh snapshot.

Pending-interaction snapshots, pages, and `ui_snapshot`/`ui_request`/
`ui_resolved` events carry a server-owned generation. Supported interaction
kinds include confirmation, input, selection, guided questions, and remote URL
opening. Clients pass the expected generation while paging and restart when the
server marks a page stale. Capability limits cover
attachment quotas and upload concurrency, page sizes, snapshot bounds, event
retention, and chunk recovery.

## Event sequencing and reconnection

Web events include one host-instance `streamId` and a monotonically increasing
`sequence`. The same shared event has the same sequence for every client;
connection-scoped command responses are not sequenced. A browser client that
batches sequenced records must reduce records received earlier on the socket
before resolving a later command response; otherwise response-driven pagination
or recovery can commit stale state. Web capability responses advertise
`eventSequencing`, the current stream and sequence, and retention limits. Stdio
capabilities report sequencing as unsupported.

Every WebSocket connection begins with `sync` and ends synchronization with
`sync_complete` before live delivery starts. With no resume cursor, or when a
cursor is invalid, stale, or from another host instance, `sync` uses
`mode: "snapshot"` and includes current state, pending interactions, plugin
header/footer chrome, and a bounded transcript tail. Snapshots retain at most the latest 200 messages and
64 KiB; `messageOffset`, `totalMessageCount`, and `messagesTruncated` identify
missing history, which clients retrieve through paginated `get_messages`
commands. Web message pages are also capped at 64 KiB. An individually oversized
message becomes a `message_reference` and is reconstructed through bounded
`get_message_chunk` responses. References preserve the full message's
`messageId`, `turnId`, and role. Reference tokens address immutable browser-safe
serialized bytes in a 16 MiB bounded cache, so transcript mutation cannot mix
chunk versions and chunks never restore projected image data or server paths.
Evicted tokens are rejected; a single projected message above the cache limit is
reported as `message_unavailable`. Snapshots include the active identified assistant partial even though it is not
yet committed to session storage, and may represent it with a reference. Clients
retain it as the active message and buffer
live continuation deltas until the immutable snapshot reference is hydrated. If
the token has expired, a fresh message page is already rebased to current server
state and buffered deltas must not be applied to it again.

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

The broker assigns a monotonically increasing pending-interaction `generation`.
Snapshots, `get_pending_interactions` pages, `ui_snapshot`, `ui_request`, and
`ui_resolved` records carry it. `ui_snapshot` authoritatively replaces pending
state for direct client attachment; request/resolution events represent one
generation increment each. Page commands may provide their expected generation; a page
from another generation returns `stale: true` without collection items so the
client restarts from current state instead of merging shifted offsets.

Snapshot and listing records replace an interaction payload over the advertised
inline limit with a reference. Clients reconstruct it through
`get_pending_interaction_chunk`; they can page omitted interaction listings
through `get_pending_interactions`. Interaction recovery enforces the advertised
per-chunk and total serialized-byte limits.

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

`get_capabilities.limits` advertises attachment count and byte quotas, upload
concurrency, message and interaction page sizes, snapshot bounds, and message
and interaction recovery limits. Web transport limits override broader shared
RPC defaults where necessary. Clients should use these values rather than copy
server constants.

Accepted remote images retain their attachment id until the client deletes its
download copy or the web host stops; deleting the copy does not release the
accepted-history budget. WebSocket records omit inline image base64 and report
`dataOmitted: true` with `attachmentId` and image metadata instead; clients
render the image through the authenticated/trusted HTTP boundary. This keeps
uploads and persisted image messages from exceeding WebSocket backpressure
limits.

## Example

```text
> {"id":"prompt-1","type":"prompt","message":"Summarize this repository"}
< {"id":"prompt-1","type":"response","command":"prompt","success":true}
< {"type":"agent.turn.started","turnId":"turn-1"}
< {"type":"user.message.created","turnId":"turn-1","messageId":"message-1","message":{"role":"user","content":"Summarize this repository","turnId":"turn-1","messageId":"message-1"}}
< {"type":"session.message.appended","turnId":"turn-1","messageId":"message-1","message":{"role":"user","content":"Summarize this repository","turnId":"turn-1","messageId":"message-1"}}
< {"type":"agent.start"}
< {"type":"agent.message.started","turnId":"turn-1","messageId":"message-2","message":{"role":"assistant","content":[],"turnId":"turn-1","messageId":"message-2"}}
< {"type":"agent.message.updated","turnId":"turn-1","messageId":"message-2","update":{"kind":"content.delta","contentType":"text","contentIndex":0,"delta":"..."}}
< {"type":"agent.message.ended","turnId":"turn-1","messageId":"message-2","message":{"role":"assistant","content":[{"type":"text","text":"..."}],"turnId":"turn-1","messageId":"message-2"}}
< {"type":"agent.end","willRetry":false}
< {"type":"agent.turn.completed","turnId":"turn-1"}
< {"type":"agent.settled"}
```
