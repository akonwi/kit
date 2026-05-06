# AgentRuntime extraction for reusable sub-agent runtimes

## Summary

Refactor `src/runtime/agent-runtime.ts` so the core execution/runtime machinery can be reused by:

- the main session runtime
- one or more sub-agent runtimes

This is explicitly **follow-up work**, not a blocker for current sub-agent v1 delivery.

## Why

The current sub-agent implementation proves the v1 product model, but it runs through a custom `KitAgent`-based executor in `src/features/subagents/state.ts` instead of reusing `AgentRuntime` directly.

That is acceptable for now, but longer term we want to avoid maintaining two orchestration paths.

## Goal

Make it possible to have multiple runtime instances backed by the same core runtime machinery, with different persistence/event policies.

Concretely, we want to support:

- one main runtime instance for the user-facing session
- multiple sub-agent runtime instances for isolated delegated conversations

## Current coupling to extract

`AgentRuntime` currently mixes several concerns:

1. agent execution lifecycle
   - `KitAgent` ownership
   - model setup
   - tool setup
   - system prompt setup
   - retries / abort / streaming

2. persistence policy
   - normal turn persistence
   - session metadata persistence
   - compaction integration

3. runtime event policy
   - top-level runtime events for the main session
   - assumptions about normal main-session turns

4. main-session ownership concerns
   - session switching
   - new session creation
   - handoff / merge-up
   - active-session lifecycle

Sub-agents want most of the execution lifecycle, but not the main-session-specific ownership and persistence behavior.

## Desired direction

Extract the reusable runtime core from `AgentRuntime`, so the remaining main runtime class is mostly a host/owner around that core.

A likely end state is:

- shared execution core
- pluggable persistence/event adapter
- main-runtime host for session switching / handoff / merge-up
- sub-agent runtime host for delegated isolated execution

## Likely extraction seams

### 1. Runtime configuration assembly

Extract logic for:

- effective system prompt construction
- context file application
- model resolution
- tool resolution
- retry configuration

### 2. Execution lifecycle around `KitAgent`

Extract logic for:

- creating and configuring `KitAgent`
- subscribing to `KitAgent` events
- running prompts
- abort / idle handling
- translating low-level execution events into higher-level callbacks

### 3. Persistence/event sinks

Introduce an adapter boundary for how runtime activity is recorded.

Examples:

- main runtime sink:
  - persist normal turns
  - emit current top-level runtime events
- sub-agent sink:
  - persist `subagent_*` entries into the parent session log
  - avoid normal top-level turn persistence

### 4. Session host responsibilities

Keep these outside the reusable core:

- `newSession`
- `switchSession`
- `handoffSession`
- `mergeUp`
- active-session ownership in the app shell

## Non-goal

This refactor does **not** require sub-agents to become separate on-disk sessions.

The single parent-session JSONL persistence model should remain intact.

## Success criteria

- sub-agent execution no longer needs a custom standalone `KitAgent` orchestration path
- main and sub-agent runtimes share the same execution machinery
- persistence policy is injectable/configurable
- main-session-only concerns remain outside the reusable core

## Notes

This should stay behind current sub-agent product work in priority. It is an architectural cleanup and reuse task, not part of the minimum v1 behavior surface.
