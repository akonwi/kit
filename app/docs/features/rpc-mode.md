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

The current protocol version is 1. Send `get_capabilities` to discover the
commands supported by the running Kit version.

| Command | Important fields | Behavior |
| --- | --- | --- |
| `get_capabilities` | | Return protocol version and command names. |
| `prompt` | `message`, optional `streamingBehavior` | Start a run, or steer/follow up while streaming. |
| `steer` | `message` | Inject steering into the active run. |
| `follow_up` | `message` | Queue a follow-up message. |
| `abort` | | Abort active work and wait for accepted runs to stop. |
| `new_session` | | Create and activate a session. |
| `get_state` | | Return model, thinking, streaming, session, cwd, and message counts. |
| `get_messages` | | Return the active transcript messages. |
| `get_last_assistant_text` | | Return the last assistant message's text. |
| `get_available_models` | | Return authenticated models. |
| `set_model` | `provider`, `modelId` | Change model while idle. |
| `get_available_thinking_levels` | | Return thinking levels for the active model. |
| `set_thinking_level` | `level` | Change the thinking level. |
| `switch_session` | `sessionPath` | Open an existing session while idle. |

Commands are serialized by one authoritative session host. Different network
clients may reuse the same command id because response correlation is scoped to
the connection that sent the command.

## Events

Kit currently publishes these event types:

- `agent_start`, `agent_end`, and `agent_settled`
- `turn_start` and `turn_end`
- `message_start`, `message_update`, and `message_end`
- `tool_execution_start`, `tool_execution_update`, and `tool_execution_end`
- `queue_update`
- `auto_retry_start` and `auto_retry_end`
- `state_changed`
- `error`

`state_changed` carries a fresh state snapshot after shared state mutations such
as session, model, or thinking-level changes. Web mode broadcasts runtime events
to every connected client.

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
GET /api/health       process health and connected-client count
WS  /api/rpc          commands, responses, and events
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

The initial web transport establishes the shared host, HTTP lifecycle, and
multi-client WebSocket behavior. The browser route is currently a placeholder.
Before web mode reaches feature parity it still needs:

- the Mica-based transcript and composer client
- remote confirm, input, select, guided-question, and approval interactions
- external-plugin initialization
- remote-safe command execution and opaque session-id operations
- attachment upload and prompt references
- sequenced event replay and snapshot-based reconnection

See ADR 0027 and `backlog/remote-session-server.md` for the intended design and
delivery sequence.

## Related

- `docs/adrs/0025-json-rpc-plugin-protocol.md`
- `docs/adrs/0026-headless-rpc-mode.md`
- `docs/adrs/0027-remote-session-server.md`
- `backlog/remote-session-server.md`
