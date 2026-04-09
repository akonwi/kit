# Architecture: Standalone Kit Shell

- Status: Accepted
- Date: 2026-03-13
- Updated: 2026-04-07
- Scope: standalone `kit` app architecture

## Summary

`kit` is a standalone coding-agent application.

It is **not** a Pi extension pack and it no longer aims for Pi storage/session
compatibility. The app owns its shell, runtime wiring, settings, auth, session
format, and tools.

The current foundation is:

- `@mariozechner/pi-agent-core` for the core agent loop and message/tool types
- `@mariozechner/pi-ai` for provider/model access
- `@opentui/core` + `@opentui/solid` for the TUI shell
- app-owned storage under `~/.kit/`

## Current architectural layers

### 1. Runtime layer

Owns agent execution and persistence boundaries.

Key modules:

- `src/runtime/kit-agent.ts`
  - `KitAgent extends Agent`
  - applies kit defaults (base system prompt, thinking level, steering/follow-up mode)
  - tracks explicit `Turn[]`
  - tags each committed message with `turnId`
- `src/runtime/agent-runtime.ts`
  - app-facing runtime wrapper
  - session switching/creation
  - context-file discovery and effective system-prompt composition
  - persistence on turn completion
  - runtime event emission for the UI
- `src/context/agents.ts`
  - discovers `~/.kit/AGENTS.md`
  - walks project ancestors for `AGENTS.md` / `CLAUDE.md`
  - renders discovered guidance into the final system prompt

### 2. Session layer

Owns the on-disk session model.

Key modules:

- `src/session/types.ts`
- `src/session/storage.ts`

Sessions are stored as JSON files at:

```text
~/.kit/sessions/<id>.json
```

The session model is turn-first:

```ts
interface Session {
  id: string;
  version: 1;
  cwd: string;
  name?: string;
  model?: string;
  createdAt: string;
  updatedAt: string;
  turns: Turn[];
}

interface Turn {
  id: string;
  messages: KitAgentMessage[];
}

type KitAgentMessage = AgentMessage & {
  turnId: string;
};
```

This means:

- turns are explicit, not heuristically reconstructed from flat messages
- every persisted message knows which turn it belongs to
- transcript presentation can render from turns directly

### 3. State layer

Bridges runtime events into reactive UI state.

Key modules:

- `src/state/app-state.ts`
- `src/state/palette-manager.ts`

Responsibilities:

- hold `turns`, panel state, footer state, toasts, session metadata
- subscribe to `AgentRuntimeEvent`
- translate runtime changes into reactive updates for the shell

### 4. Shell layer

Owns the TUI presentation.

Key modules:

- `src/shell/AppShell.tsx`
- `src/shell/TranscriptPane.tsx`
- `src/shell/ComposerDock.tsx`
- `src/shell/InlinePicker.tsx`
- `src/shell/ToastStack.tsx`
- `src/shell/BottomStatusBar.tsx`

The shell is app-owned and uses OpenTUI primitives directly.

Important current UX decisions:

- transcript renders from explicit turns
- assistant text is not streamed token-by-token into the transcript
- runtime activity is surfaced through ephemeral UI state (panel, toasts, tool rows)
- notices are ephemeral toasts, not persistent transcript entries

### 5. Feature layer

Owns app behavior above the shell/runtime foundation.

Current or in-progress areas:

- slash commands
- file references
- thread references
- pager
- guided questions
- handoff
- subagent

## Runtime event pattern

The backend communicates with the app through a subscription model.

Current important events include:

- `turns_changed`
- `status_changed`
- `session_changed`
- `panel`
- `tool_completed`
- `turn_complete`
- `pending_changed`
- `error`
- `info`

Flow:

```text
KitAgent / AgentRuntime
  -> emits runtime events
AppState
  -> updates reactive store
Shell
  -> renders current state
User actions
  -> call runtime / palette actions
```

## Shell model

The shell is built around explicit regions:

1. transcript/main content
2. fixed bottom composer/dock
3. inline picker / overlay layer
4. ephemeral toast layer

This is intentionally different from Pi interactive mode. The app owns layout,
focus, and interaction patterns directly.

## What is no longer true

Older docs and plans assumed:

- Pi session compatibility
- Pi settings fallback
- a `compat/` layer around Pi storage/contracts
- `pi-kit` naming
- backend structure under `src/backend/`

Those assumptions are obsolete.

Current direction instead:

- standalone `kit`
- no Pi compatibility requirement
- settings only from `~/.kit/settings.json`
- auth only from `~/.kit/auth.json`
- sessions only from `~/.kit/sessions/*.json`
- flat `src/runtime/` structure

## Current priorities

1. keep the minimum working agent loop solid
2. continue rebuilding disabled features on top of the new runtime/state model
3. remove remaining legacy `@ts-nocheck` / structural type debt
4. refine shell UX incrementally without reintroducing compatibility baggage

## Decision

`kit` continues as a standalone coding-agent shell with:

- app-owned storage
- app-owned shell
- app-owned runtime wiring
- `pi-agent-core` as the loop foundation
- explicit turn-based persistence and transcript rendering
