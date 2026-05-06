# Sub-agents

Sub-agents are a planned Kit feature for delegating work into isolated agent contexts.

This document describes the intended v1 design, not the current implementation.

## Goal

Kit should support named sub-agents that are:

- user-declared
- reusable across sessions
- callable by the main agent through a built-in tool
- isolated from the main agent's context window
- resumable across multiple delegated turns

The primary goal is to reduce context bloat in the main agent without reducing delegated work to one-shot RPC calls.

## Why sub-agents exist

Sub-agents should help when the main agent would otherwise have to spend context on:

- reconnaissance
- planning
- specialized review
- iterative delegated work that benefits from follow-up

Examples of useful roles:

- `scout` for fast codebase reconnaissance
- `reviewer` for correctness and risk review
- `planner` for implementation plans

## Core product principles

### Context isolation

A sub-agent runs in its own isolated agent context.

The main session should not automatically absorb the sub-agent's full transcript into the main agent's context window.

### Resumable delegation

Delegation is not limited to one-shot request/response flows.

The main agent should be able to:

- start work with a named sub-agent
- receive an initial response
- send follow-up questions
- continue that delegated conversation later

### Shared definitions

Sub-agents come from user-owned configuration rather than hardcoded built-ins.

The same declared agent should be reusable whenever the main agent wants to delegate that role.

### One session record on disk

Sub-agent activity should be persisted in the same main session log.

Kit should not create a second persisted session file just for sub-agent history.

## User interaction model

There is no dedicated composer syntax for sub-agents in v1.

Users request delegation in normal language, for example:

- `Use scout to find the auth entry points`
- `Ask reviewer to inspect this diff for correctness`
- `Continue with scout and focus on OAuth state persistence`
- `Dismiss scout and start over`

The main agent interprets that request and decides whether to call the `subagent` tool.

That means v1 has:

- no `@agent`
- no `@@agent`
- no sub-agent picker
- no special composer tokenization
- no submit-time routing shortcut

## Config and discovery

Sub-agents are declared as markdown files with YAML frontmatter.

### Discovery locations

Kit-native locations:

1. `~/.kit/agents/*.md`
2. `.kit/agents/*.md`

Pi-subagents compatibility locations:

3. `~/.pi/agent/agents/*.md`
4. `.pi/agents/*.md`

### Precedence

Use the discovery order above.

First-loaded wins on name collisions.

### Discovery behavior

- non-recursive
- direct `*.md` files only

### Supported frontmatter

- `name` — required
- `description` — required
- `model` — optional

The markdown body is the sub-agent's instructions.

Example:

```md
---
name: scout
description: Fast codebase reconnaissance agent
model: claude-haiku-4-5
---

You are Scout.

Focus on reconnaissance and compressed findings.
Prefer concise outputs with concrete file paths and bullets.
Do not make edits unless explicitly instructed.
```

### Out of scope for v1 config

- `tools` frontmatter
- project-level trust gating

## Main-agent tool surface

Kit should expose one built-in tool:

- `subagent`

### Input

```ts
export type SubagentToolInput =
  | { action: "list_agents" }
  | { action: "run"; agent: string; message: string }
  | { action: "status"; agent: string }
  | { action: "dismiss"; agent: string };
```

### Output

```ts
export type SubagentToolOutput =
  | {
      ok: true;
      action: "list_agents";
      agents: Array<{
        name: string;
        description: string;
        model?: string;
        source: "kit-user" | "kit-project" | "pi-user" | "pi-project";
      }>;
    }
  | {
      ok: true;
      action: "run";
      agent: string;
      status: "completed" | "failed" | "aborted";
      message?: string;
      error?: string;
    }
  | {
      ok: true;
      action: "status";
      agent: string;
      active: boolean;
      status?: "idle" | "running" | "failed" | "aborted";
      model?: string;
      lastActivityAt?: string;
      latestMessage?: string;
    }
  | {
      ok: true;
      action: "dismiss";
      agent: string;
      dismissed: boolean;
    }
  | {
      ok: false;
      action: "list_agents" | "run" | "status" | "dismiss";
      code:
        | "SUBAGENT_NOT_FOUND"
        | "SUBAGENT_BUSY"
        | "INVALID_INPUT"
        | "RUNTIME_ERROR";
      message: string;
    };
```

### Tool semantics

- `list_agents` returns discovered sub-agent definitions
- `run` sends a message to the named sub-agent
  - if the agent has no active conversation, create one
  - otherwise continue the active one
- `status` inspects the current active conversation for that agent
- `dismiss` resets that agent's active conversation

The main agent should not need to reason about raw sub-agent session ids.

## Runtime model

### Active state

In memory, keep a thin map of:

- `agent -> active conversation`

Conceptually:

```ts
interface SessionSubagentManager {
  conversationsByAgent: Map<string, ActiveSubagentConversationState>;
}
```

```ts
interface ActiveSubagentConversationState {
  agentName: string;
  subagentConversationId: string;
  status: "idle" | "running" | "failed" | "aborted";
  model?: string;
  description?: string;
  lastActivityAt: string;
  runtime?: LiveSubagentRuntime;
  failure?: SubagentFailedEntry;
  abort?: SubagentAbortedEntry;
}
```

### Runtime rules

- one active conversation per agent per main session
- one in-flight run per active agent
- if an active sub-agent is already running, `run` returns `SUBAGENT_BUSY`
- completion does not reset the conversation
- only `dismiss` resets it
- failed or aborted conversations remain active until dismissed
- sub-agents cannot call `subagent` themselves in v1

## Persistence model

Sub-agent activity is persisted in the main session JSONL.

There should be one on-disk session record, not separate persisted sub-agent session files.

Each sub-agent event should include:

- `agentName`
- `subagentConversationId`

### Persisted event types

Lifecycle:

- `subagent_started`
- `subagent_dismissed`

Inputs:

- `subagent_prompt`

Assistant output:

- `subagent_message_started`
- `subagent_message_delta`
- `subagent_message_completed`

Thinking:

- `subagent_thinking_started`
- `subagent_thinking_delta`
- `subagent_thinking_completed`

Tool activity:

- `subagent_tool_started`
- `subagent_tool_updated`
- `subagent_tool_completed`

Terminal state:

- `subagent_failed`
- `subagent_aborted`

`subagent_result` is intentionally omitted. The meaningful output already exists as `subagent_message_completed`.

Persist the full sub-agent event stream even if first-pass UI only renders a subset.

## Main-session UX implications

Sub-agent activity should contribute to the main session history so the app can later support:

- seeing that delegation happened
- exploring sub-agent activity
- inspecting prior delegated work within the app

At the same time, low-level sub-agent events should not automatically bloat the main agent's prompt context.

The important split is:

- persist broadly for history and reconstruction
- render selectively for UX
- include narrowly in the main model context

## Relationship to `/handoff`

Kit already has `/handoff`, which creates a linked child session and switches the user into it.

Sub-agents should not simply be treated as automated handoff.

`/handoff` is best thought of as:

- explicit user branching
- full copied history
- later merge-up via summary

Sub-agents are intended to be:

- named specialized workers
- main-agent-directed delegation
- isolated contexts
- resumable without switching the user's primary session by default

Implementation details may overlap with session lineage and merge concepts, but the product behavior should remain distinct.

## Non-goals for v1

The first pass should not try to solve every multi-agent workflow.

Out of scope:

- special composer syntax for sub-agents
- transcript export as a separate tool action
- batch or parallel sub-agent APIs
- nested sub-agent delegation
- separate persisted sub-agent session files
- config-level tool restrictions in agent frontmatter

## Current status

Sub-agents are not implemented yet.

Current related features are:

- `/handoff`
- child-session merge-up
- thread references

Those features are useful adjacent pieces, but they are not a replacement for first-class sub-agent support.
