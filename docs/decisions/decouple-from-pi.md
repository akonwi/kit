# Decision: Full decoupling from pi-coding-agent

## Status

Decided — work in progress

## Context

`pi-kit` started as a Pi extension pack, then became a standalone app that reused
Pi's session format and `pi-coding-agent` internals for session management,
agent orchestration, and tooling. The goal was Pi compatibility.

That goal is now dropped. Full independence is preferred over compatibility.

## Decision

Remove `@mariozechner/pi-coding-agent` entirely. Keep only:

- `@mariozechner/pi-agent-core` — agent loop, tool types, message types
- `@mariozechner/pi-ai` — AI provider abstraction (Anthropic, etc.)
- `@opentui/solid` + `@opentui/core` — TUI rendering

## What needs to be replaced

### 1. Session storage (highest impact)

**Currently:** `SessionManager` from `pi-coding-agent` reads/writes Pi's JSONL session format.

**Replace with:** Own session format. Proposed: one JSON file per session in `~/.pi-kit/sessions/`.

```
~/.pi-kit/
  sessions/
    <id>.json       # full session: header + messages array
  settings.json
  agents/           # user-scope agent definitions
```

Session file shape:
```json
{
  "id": "uuid",
  "version": 1,
  "cwd": "/path/to/project",
  "name": "optional display name",
  "createdAt": "iso8601",
  "updatedAt": "iso8601",
  "model": "claude-sonnet-4",
  "messages": [ ...AgentMessage[] ]
}
```

### 2. Agent runtime

**Currently:** `AgentSession` / `createAgentSession` from `pi-coding-agent` — wraps the agent loop
with session persistence, compaction, tool registration, etc.

**Replace with:** Thin `AgentRuntime` class around `pi-agent-core`'s `Agent` that:
- Holds messages in memory
- Persists to our session file on each turn
- Handles abort / streaming events

### 3. Tools

**Currently:** `createBashTool`, `createReadTool`, `createWriteTool`, etc. from `pi-coding-agent`.

**Replace with:** Own implementations. These are straightforward wrappers around
`fs` / `child_process` that return `AgentTool`-shaped objects.

### 4. Settings

**Currently:** `SettingsManager` from `pi-coding-agent`.

**Replace with:** Already partially done — `~/.pi-kit/settings.json` with own loader.
Finish removing the Pi settings dependency.

### 5. Subagent runner

**Currently:** `createAgentSession` to spawn an in-process subagent.

**Replace with:** Spawn `pi-kit` CLI as a subprocess (same approach as the old
extension's `pi` subprocess runner).

### 6. Session loader / thread index

**Currently:** Reads Pi's JSONL session files from `~/.pi/agent/sessions/`.

**Replace with:** Read our own `~/.pi-kit/sessions/*.json` files.

### 7. `generateSummary` (handoff)

**Currently:** Imported from `pi-coding-agent`.

**Replace with:** Direct LLM call via `pi-agent-core`'s `Agent` with a summarization prompt.

### 8. `getAgentDir`, `parseFrontmatter` (subagent agents)

**Currently:** Imported from `pi-coding-agent`.

**Replace with:** Own path helper (`~/.pi-kit/agents/`) + simple YAML frontmatter parser.

## Keep

- `pi-agent-core`: `Agent`, `agentLoop`, `AgentMessage`, `AgentTool`, `AgentEvent`, `ThinkingLevel`
- `pi-ai`: `streamSimple`, `Model`, `Api`, provider registration, `ApiRegistry`

## Migration order

1. Own session format + reader/writer (`src/session/`)
2. Own tool implementations (`src/tools/`)
3. Own `AgentRuntime` using `pi-agent-core`'s `Agent`
4. Own settings (`src/settings/`)
5. Wire up session loader, thread index to new format
6. Replace subagent runner
7. Replace `generateSummary`
8. Replace `getAgentDir` / `parseFrontmatter`
9. Remove `pi-coding-agent` from `package.json`
