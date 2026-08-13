# ADR 0026: Headless RPC mode

## Status

Accepted

## Context

Print mode already runs Kit without OpenTUI, but it accepts one prompt, emits
only final assistant text, and exits. Process hosts need a long-lived Kit
instance that can accept multiple prompts, observe streaming work, control the
active run, and retain conversation state.

Pi exposes this capability as newline-delimited JSON commands, responses, and
events over stdio. Kit already uses a different JSON-RPC 2.0 protocol for
external plugins; that protocol models Kit as the client of a plugin process
and is not the right ownership or method surface for controlling Kit itself.

## Decision

Kit provides `--rpc` as a JSONL subprocess protocol. Its framing and
command/response envelope are Pi-inspired, but Pi is not part of Kit's public
protocol boundary. Interactive, `--print`, and `--rpc` modes are mutually
exclusive.

- stdin carries commands; stdout carries responses and runtime events; stderr
  carries diagnostics.
- Prompt acceptance is asynchronous. Completion is represented by
  `agent.settled`, not by the prompt response.
- RPC and print mode share one headless host that owns runtime and built-in
  plugin initialization and cleanup.
- RPC mode creates a persistent session by default, can open one with
  `--session`, and supports ephemeral operation through `--no-session`.
- The initial command surface exposes only behavior Kit can support through
  existing `AgentRuntime` interfaces. Unsupported Pi commands are explicit
  errors rather than compatibility shims.
- Protocol records follow Pi's command/response/event envelope rather than
  JSON-RPC 2.0. The external-plugin protocol remains unchanged.
- Event records project Kit's semantic `AgentRuntime` events and preserve their
  identities and lifecycle meaning. Raw Pi event names, payloads, and types do
  not cross the core `Agent` boundary. Transport projections may bound or omit
  unsafe data, but they do not make Pi's event model part of Kit's API.

## Consequences

Process hosts can use Kit without a terminal renderer and can stream messages
and tool execution while retaining request correlation. stdout must remain
protocol-clean for the process lifetime. Adding commands or events is a public
protocol change and should be reflected in `docs/features/rpc-mode.md` and
covered at the framing/dispatch boundary.

Kit is not yet a drop-in implementation of Pi's complete RPC surface. Session
tree operations, direct bash RPC, manual compaction, cycle commands, and an
interactive extension-UI bridge can be added later through existing runtime
interfaces or deliberate new ones.
