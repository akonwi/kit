# RPC mode

Kit can run as a long-lived headless subprocess using newline-delimited JSON on
stdin and stdout:

```bash
kit --rpc
kit --rpc --no-session
kit --rpc --session <id-or-path>
kit --rpc --model <provider>/<model-id>
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

RPC mode creates and persists a new Kit session by default. `--session` opens
an existing session. `--no-session` keeps the main conversation and sub-agent
conversations in memory.

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
| `abort` | none | none |
| `new_session` | none | `{ "cancelled": false }` |
| `get_capabilities` | none | protocol version, features, and transport limits |
| `get_state` | none | current model, thinking level, session, CWD, streaming, and message counts |
| `get_messages` | none | `{ "messages": [...] }` |
| `get_last_assistant_text` | none | `{ "text": string \| null }` |
| `get_available_models` | none | `{ "models": [...] }` |
| `set_model` | `provider`, `modelId` | selected model |
| `get_available_thinking_levels` | none | `{ "levels": [...] }` |
| `set_thinking_level` | `level` | none |
| `switch_session` | `sessionPath` (a Kit session ID or path) | `{ "cancelled": false }` |
| `list_commands` | none | transport-neutral commands plus `registryGeneration` |
| `execute_command` | `commandId`, optional `args`, optional `registryGeneration` | none |

Clients that present commands interactively should pass the generation returned
by `list_commands` to `execute_command`. Kit rejects stale generations when the
command registry changes, preventing a displayed command from resolving to a
newly registered implementation with the same ID. Generations are scoped to one
RPC host incarnation, so clients must discard them after reconnecting to a new
event stream. The generation remains optional for compatibility with
controllers that execute a known command ID directly.

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
- `chat.message-queue.changed`
- `agent.retry.started`, `agent.retry.failed`, and `agent.retry.completed`
- `agent.run.failed` for an execution failure after a command was accepted

Every transcript message has a stable `turnId` and `messageId`.
`agent.message.updated.update` is a Kit-owned content update with a `kind`,
`contentType`, and `contentIndex`; content deltas also include `delta`.
`agent.message.ended.message` is the authoritative completed assistant message.
`session.message.appended.message` is the authoritative committed transcript
message and also covers non-assistant messages such as tool results.
`session.transcript.replaced` requires clients to obtain a fresh snapshot.

Pending-interaction snapshots, pages, and `ui_snapshot`/`ui_request`/
`ui_resolved` events carry a server-owned generation. Clients pass the expected generation while
paging and restart when the server marks a page stale. Capability limits cover
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
