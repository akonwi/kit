# RPC mode

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
subagents, system-prompt slots, and lifecycle events are active. Commands and
chrome can register but do not currently have an RPC presentation or execution
surface. Plugin confirm, input, and select requests cannot display a terminal
dialog; they fail or return cancellation.

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
aliases, and each request's same origin. Extra reverse-proxy or tunnel addresses
can be added without losing local browser access:

```sh
kit --web \
  --auth 'username:password' \
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
required to carry an Origin. Kit warns when
a wildcard is used without `--auth`. Even with authentication, only use an
origin wildcard behind HTTPS and a trusted network or access-control proxy,
because it disables Kit's cross-site request protection.

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

| Command | Fields | Result data |
| --- | --- | --- |
| `prompt` | `message`, optional `streamingBehavior: "steer" \| "followUp"` | none; accepted asynchronously |
| `steer` | `message` | none |
| `follow_up` | `message` | none |
| `restore_follow_ups` | `clientId`, `operationId`, `sessionId`, `expectedGeneration` | ordered text-only `messages` plus the resulting queue `generation` |
| `promote_follow_ups` | `sessionId`, `expectedGeneration` | promoted `count` plus the resulting queue `generation`; active runs only |
| `acknowledge_follow_up_mutation` | `clientId`, `operationId` | release a retained non-empty restore after applying it safely |
| `abort` | none | none |
| `new_session` | none | `{ "cancelled": false }` |
| `get_capabilities` | none | protocol version, features, and transport limits |
| `get_state` | none | current model, thinking level, session, CWD, streaming, message counts, queued follow-up previews, and context usage |
| `get_messages` | none | `{ "messages": [...] }` |
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

RPC mode emits Kit semantic events using the same dotted names as the runtime:

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
