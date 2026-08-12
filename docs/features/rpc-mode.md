# RPC mode

Kit can run as a long-lived headless subprocess using newline-delimited JSON on
stdin and stdout:

```bash
kit --rpc
kit --rpc --no-session
kit --rpc --session <id-or-path>
kit --rpc --model <provider>/<model-id>
```

RPC mode is inspired by Pi's RPC mode and follows its command, response, and
event envelope conventions. It is not currently a complete implementation of
Pi's command surface.

## Transport and lifecycle

- Write one JSON command per line to stdin. LF and CRLF are accepted, as is a
  final record without a trailing newline.
- Read one JSON response or event per line from stdout.
- stdout is reserved for protocol records. Kit logs and diagnostics go to
  stderr.
- Responses and events can interleave while accepted prompts execute. Include
  a string `id` to correlate a response with its command.
- A successful `prompt` response means the prompt was accepted, not that the
  model has completed. Wait for `agent_settled` and inspect `message_end` or
  call `get_last_assistant_text` for the final result.
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

Like print mode, RPC mode loads only headless-safe built-in plugins. User and
project plugins and UI-only tools are not loaded.

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
| `get_state` | none | current model, thinking level, session, CWD, streaming, and message counts |
| `get_messages` | none | `{ "messages": [...] }` |
| `get_last_assistant_text` | none | `{ "text": string \| null }` |
| `get_available_models` | none | `{ "models": [...] }` |
| `set_model` | `provider`, `modelId` | selected model |
| `get_available_thinking_levels` | none | `{ "levels": [...] }` |
| `set_thinking_level` | `level` | none |
| `switch_session` | `sessionPath` (a Kit session ID or path) | `{ "cancelled": false }` |

A normal `prompt` is rejected while the agent is streaming unless
`streamingBehavior` says where to queue it. The dedicated `steer` and
`follow_up` commands provide the same queue controls directly. `set_model`,
`new_session`, and `switch_session` are also rejected while the agent is
streaming; successful responses wait for any model-adaptation compaction to
finish before the next command is dispatched.

## Events

RPC mode currently emits:

- `agent_start`, `agent_end`, and `agent_settled`
- `turn_start` and `turn_end`
- `message_start`, `message_update`, and `message_end`
- `tool_execution_start`, `tool_execution_update`, and `tool_execution_end`
- `queue_update`
- `auto_retry_start` and `auto_retry_end`
- `error` for an execution failure after a command was accepted

`message_update.assistantMessageEvent` contains the model stream delta.
`message_end.message` is the authoritative completed message.

## Example

```text
> {"id":"prompt-1","type":"prompt","message":"Summarize this repository"}
< {"id":"prompt-1","type":"response","command":"prompt","success":true}
< {"type":"agent_start"}
< {"type":"turn_start"}
< {"type":"message_start","message":{"role":"assistant","content":[]}}
< {"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"..."}}
< {"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"..."}]}}
< {"type":"agent_end","messages":[...],"willRetry":false}
< {"type":"agent_settled"}
```
