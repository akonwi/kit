# Sub-agents

## Summary

Add first-class sub-agent support to Kit.

Sub-agents should be:

- user-declared
- callable by the main agent through a built-in tool
- context-isolated
- resumable across multiple turns
- persisted in the main session log without separate on-disk child session copies

The primary goal is to reduce context bloat in the main agent while preserving a usable history of delegated work inside the app.

## Settled v1 direction

### Config and discovery

Sub-agent definition files should be discovered from:

1. `~/.kit/agents/*.md`
2. `.kit/agents/*.md`
3. `~/.pi/agent/agents/*.md`
4. `.pi/agents/*.md`

Rules:

- discovery is non-recursive
- direct `*.md` files only
- first-loaded wins on name collisions
- supported frontmatter is:
  - `name` required
  - `description` required
  - `model` optional
- the markdown body is the sub-agent instructions
- no `tools` frontmatter in v1
- no project-level trust gating in v1

### User interaction

There is no special composer syntax for sub-agents in v1.

Users request delegation in normal language and the main agent decides whether to call the `subagent` tool.

That means:

- no `@agent`
- no `@@agent`
- no dedicated picker
- no special tokenization or submit-time interception

### Tool surface

Use one built-in tool:

- `subagent`

Tool actions:

- `list_agents`
- `run`
- `status`
- `dismiss`

`run` is create-or-continue by agent name.

The main agent should not need to reason about raw sub-agent session ids.

### Runtime model

Keep a thin in-memory map of:

- `agent -> active conversation`

Only active conversations stay in memory.

- one active conversation per agent per main session
- one in-flight run per active agent
- completion does not reset the conversation
- only `dismiss` resets it
- failed or aborted conversations remain active until dismissed
- sub-agents cannot call `subagent` themselves in v1

### Persistence model

Persist sub-agent activity into the main session JSONL.

There should be one on-disk session record, not separate persisted sub-agent session files.

Each sub-agent event should include:

- `agentName`
- `subagentConversationId`

Persist the full sub-agent event stream, including:

- `subagent_started`
- `subagent_dismissed`
- `subagent_prompt`
- `subagent_message_started`
- `subagent_message_delta`
- `subagent_message_completed`
- `subagent_thinking_started`
- `subagent_thinking_delta`
- `subagent_thinking_completed`
- `subagent_tool_started`
- `subagent_tool_updated`
- `subagent_tool_completed`
- `subagent_failed`
- `subagent_aborted`

Do not add a separate `subagent_result` event in v1.

## Remaining implementation tasks

- polish transcript rendering and replay behavior for delegated runs
- improve post-reload continuation fidelity where resumed delegated runs still differ from uninterrupted in-memory execution
- ensure low-level sub-agent events do not automatically bloat the main agent's prompt context

## Why this is not `/handoff`

`/handoff` is still the branch-and-switch workflow:

- explicit user branching
- full copied history
- later merge-up via summary

Sub-agents are instead:

- named specialized workers
- main-agent-directed delegation
- isolated contexts
- resumable without switching the user's primary session by default

## Related

- `docs/features/subagents.md`
- `docs/features/handoff.md`
- `docs/adrs/0010-skip-child-threads.md`
- `docs/adrs/0016-child-session-merge-up.md`
