# Architecture Decision: Custom Shell App with Pi-Core Compatibility

- Status: Accepted
- Date: 2026-03-13
- Updated: 2026-03-15
- Scope: `v2/` standalone app architecture

## Summary

`v2/` is a **new application**, not a Pi extension pack.

The app will provide a custom terminal UX around Pi-compatible agent and session primitives, while preserving compatibility with the parts of Pi that matter most operationally:

- existing Pi session files
- core Pi agent/message/tool semantics
- baseline Pi settings and storage conventions where practical

The current Pi interactive TUI, extension lifecycle, and `pi-kit` shell architecture are **reference material**, not the runtime foundation of this new app.

The new app will have:

1. a **compatibility layer** for Pi sessions/settings/core contracts
2. a **custom shell** with a fixed bottom interaction dock and independently scrollable content regions
3. a **feature layer** that ports and evolves the useful behavior currently implemented in `pi-kit`

The likely UI implementation path is OpenTUI or another renderer with true layout and scroll-region primitives, but the architecture below is intentionally renderer-agnostic.

---

## Why this decision exists

The current `pi-kit` package successfully stretches Pi's extension system, but it is constrained by Pi interactive mode's rendering model:

- the UI is fundamentally one vertically stacked render tree
- viewport behavior follows the bottom of total rendered output
- footer/status/widget behavior changes the effective layout below the editor
- extension UI is shaped by `ctx.ui.*` constraints rather than by the needs of a standalone shell

That makes it difficult to achieve the intended UX:

- a fixed interaction dock
- independently scrollable content above it
- screen-local interactions without whole-viewport drift
- a shell that feels closer to Amp/OpenCode-class tools

Rather than continuing to patch around those constraints, `v2/` formalizes a different direction:

- keep Pi compatibility where it matters
- replace the interactive shell entirely
- port the behavior we want from `pi-kit` into a first-class app architecture

---

## Goals

### Primary goals

1. **Preserve Pi session compatibility**
   - Existing Pi sessions should remain readable and usable.
   - Branching/tree semantics should stay intact.

2. **Preserve core Pi agent compatibility**
   - Message structures, tool execution concepts, and agent/session behavior should remain recognizable and compatible where possible.

3. **Provide a true custom shell**
   - Fixed bottom interaction region
   - Independently scrollable transcript/screen regions
   - Overlay/dialog/picker model designed for this app, not inherited from Pi interactive mode

4. **Port the useful product behavior from `pi-kit`**
   - pager
   - wizard/questionnaire UX
   - thread references
   - ignore-file support
   - handoff helpers
   - other workflow affordances

5. **Keep future extensibility possible**
   - UI extensions are not an immediate priority, but the shell should not be designed in a way that makes them impossible later.

### Secondary goals

- Maintain compatibility with baseline Pi settings where feasible
- Reuse Pi storage conventions where that improves interoperability
- Avoid unnecessary divergence in data model and file formats
- Keep the app architecture modular enough to evolve independently from Pi interactive mode

---

## Non-goals

### Immediate non-goals

1. **Extension compatibility with Pi interactive mode**
   - Existing Pi UI extensions are not expected to run inside this new shell.
   - `ctx.ui.*`-style extension APIs are not part of the initial app architecture.

2. **Reusing Pi interactive mode as a host shell**
   - We are not trying to replace only pieces of Pi interactive mode.
   - We are building a new app shell.

3. **Perfect parity with Pi's current UI**
   - The goal is compatibility with Pi's core behavior and data, not reproduction of its exact interface.

4. **Immediate plugin/UI extension platform design**
   - The architecture should leave room for UI extensions in the future, but a stable extension API is explicitly deferred.

### Explicitly deferred

- public UI plugin SDK
- extension rendering APIs
- shell theming/plugin injection beyond what is required for the app itself
- compatibility shims for the full Pi extension ecosystem

---

## Compatibility contract

To keep the project focused, compatibility is divided into three buckets.

## A. Hard compatibility

These are expected to remain compatible or intentionally translated with high fidelity:

- Pi session file format
- session tree/branch semantics
- message and tool-result structures used by Pi agent flows
- baseline settings and storage paths where practical

## B. Soft compatibility

These should be preserved where helpful, but may be adapted to fit the new app:

- command names and UX affordances
- prompt discovery/loading conventions
- model/provider selection UX
- theme loading conventions
- auth/session bootstrap details

## Config and storage resolution

The new app remains named **pi-kit**.

### Pi compatibility root

For Pi-compatible state, the default compatibility root is:

- `~/.pi/agent`

This is the default location for:

- existing Pi sessions
- baseline Pi settings and supporting files
- other Pi-managed state that the new app chooses to read for compatibility

### Pi-kit app settings

The new app's own settings live at:

- `~/.pi-kit/settings.json`

These settings are **app-native** and should be used for shell/UI behavior and other pi-kit-specific configuration.

### Precedence

If both Pi baseline settings and pi-kit app settings are available, the app should prefer:

1. `~/.pi-kit/settings.json`
2. fallback values read from `~/.pi/agent` where relevant
3. built-in defaults

In other words: when both are present, **pi-kit settings win**.

This allows the new app to stay Pi-compatible by default without forcing its own UX and product behavior to live inside Pi's config namespace.

## C. Not compatible by design

These are not part of the initial app contract:

- Pi interactive-mode layout behavior
- `ctx.ui.setEditorComponent`, `setStatus`, widget/footer injection contracts
- Pi extension UI APIs
- assumptions that extension UI is rendered inside Pi's flat TUI tree

---

## Architectural layers

## 1. Compatibility layer

This layer exists to preserve interoperability with Pi-compatible state and behavior.

Responsibilities:

- session loading/saving
- branch/tree navigation compatibility
- settings/config loading
- prompt/theme discovery where desired
- storage path resolution
- adapters around Pi-core concepts

This layer should be conservative and stable.

It is not where custom UX logic belongs.

## 2. Backend layer

This layer owns agent execution and app-facing domain behavior.

Responsibilities:

- agent orchestration (`AgentRuntime`)
- tool execution integration
- model/provider interaction
- session mutation and event emission
- feature services shared by multiple screens

The backend exposes app-friendly state and events to the rest of the app via an event subscription model (see [Runtime event pattern](#runtime-event-pattern) below).

## 3. State layer

This layer owns reactive app state using Solid primitives (`createStore`, `createSignal`, `createMemo`).

Responsibilities:

- app-wide reactive state (`AppState` via `createStore`)
- palette/overlay state management (`PaletteManager` via `createSignal`)
- subscribing to backend runtime events and updating reactive state
- exposing derived/computed state for the shell to render

The state layer bridges the backend (imperative events) and the shell (reactive rendering). It does not contain rendering logic or UI components.

## 4. Shell layer

This is the custom TUI shell, built with OpenTUI/Solid.

Responsibilities:

- layout (`AppShell`)
- transcript rendering (`TranscriptPane`)
- composer input (`ComposerDock`)
- picker/overlay rendering (`InlinePicker`)
- focus management and keyboard interaction
- status display (`BottomStatusBar`)

The shell reads from reactive state and delegates actions to the backend runtime or palette manager. Picker keyboard handling (up/down/enter/escape, filtering) is owned by the `InlinePicker` component itself, keeping the `ComposerDock` focused on text composition.

This layer is free to diverge from Pi interactive mode.

## 5. Feature layer

This layer ports and evolves the behavior currently living in `pi-kit`.

Responsibilities:

- slash commands — self-contained command definitions that own their own execution and palette interaction (`src/features/commands/`)
- pager (planned)
- wizard (planned)
- thread references (planned)
- handoff flows (planned)
- ignore-file workflows (planned)

Each command is a `Command` object with `name`, `description`, and an `execute(ctx)` function that receives the runtime and palette manager. Commands handle their own UI flow (opening pickers, prompting for input) without leaking implementation details to the shell.

Features depend on backend + state abstractions, not on Pi extension APIs.

---

## Runtime event pattern

The backend `AgentRuntime` communicates state changes to the rest of the app via an event subscription model. This decouples the backend from the rendering layer.

### Event types (`AgentRuntimeEvent`)

| Event | Emitted when |
|-------|-------------|
| `messages_changed` | Messages are added, modified, or the session changes |
| `status_changed` | Model, thinking level, or context usage changes |
| `session_changed` | Session is created, switched, or renamed |
| `panel` | Tool execution starts/ends (pending indicator) |
| `error` | An error occurs during agent execution |

### Data flow

```
AgentRuntime (backend)
  │  emits AgentRuntimeEvent
  ▼
AppState (state layer)
  │  subscribes, updates createStore
  ▼
Shell components (shell layer)
  │  read reactive state, render
  ▼
User actions
  │  call runtime methods or palette manager
  ▼
AgentRuntime / PaletteManager
  │  emit events / update signals
  └──→ cycle repeats
```

The state layer subscribes to runtime events and translates them into reactive store updates. Shell components read from the store and re-render automatically via Solid's fine-grained reactivity.

Commands in the feature layer receive both the runtime and palette manager via `CommandContext`, allowing them to trigger backend operations and open UI pickers without coupling to the shell.

---

## Proposed shell model

The shell should be designed around explicit regions rather than a single stacked output document.

### Regions

1. **Header / contextual chrome** (optional, compact)
2. **Main content region**
   - transcript, screen views, pagers, review flows
   - independently scrollable
3. **Fixed interaction dock**
   - composer, wizard controls, mode-specific input surfaces
   - anchored to the bottom
4. **Overlay layer**
   - pickers, dialogs, menus, command palette, temporary panels

### Core shell concepts

- **Screen** — owns the main content region
- **Dock surface** — owns the bottom interaction area
- **Overlay** — transient, top-priority UI
- **Shell state** — active screen, active dock surface, overlay stack, focus target

This intentionally preserves useful concepts from the current `pi-kit` architecture, but moves them into a standalone app model instead of an extension-hosted shell.

---

## Relationship to current `pi-kit`

The current `pi-kit` codebase is useful in three ways:

## 1. As product behavior

It already encodes useful workflows and preferences:

- long-form pager
- questionnaire/wizard flow
- thread references
- ignore file rules
- handoff patterns
- shell shortcuts and interaction ideas

## 2. As logic to extract

Some current code is UI-agnostic or close to it and should be extracted or rewritten into reusable services:

- long-form section splitting
- feedback formatting
- ignore file parsing and management
- reference indexing/scoring helpers
- handoff summary builders

## 3. As a record of what not to carry forward

The `extensions/ui/*` architecture solved real problems inside Pi interactive mode, but it remains shaped by that host environment.

It should inform the new shell, but not define it.

---

## Proposed source structure

The new app should move away from extension-centric structure and toward app-centric structure.

Current layout:

```text
v2/
  src/
    app/
      main.tsx              # entrypoint
      bootstrap.tsx         # app initialization
      App.tsx               # root component
    compat/
      paths.ts              # Pi-compatible storage paths
      sessions/
        session-loader.ts   # session loading/snapshot
      settings/
        load-settings.ts    # settings resolution with precedence
    backend/
      runtime/
        agent-runtime.ts    # AgentRuntime — orchestration, events
    state/
      app-state.ts          # reactive AppState (createStore)
      palette-manager.ts    # PaletteManager (createSignal stack)
      palette.ts            # palette types, snapshot helpers
    shell/
      AppShell.tsx           # root layout
      ComposerDock.tsx       # composer input with slash command trigger
      InlinePicker.tsx       # picker/overlay rendering with native inputs
      TranscriptPane.tsx     # scrollable message transcript
      PendingSlot.tsx        # pending operation display
      BottomStatusBar.tsx    # footer status bar
      theme.ts               # color/style constants
    features/
      commands/
        types.ts             # Command, CommandContext
        utils.ts             # shared helpers (formatTimeAgo, etc.)
        new.ts               # /new command
        model.ts             # /model command
        thinking.ts          # /thinking command
        name.ts              # /name command
        switch.ts            # /switch command
        sessions-manage.ts   # /sessions:manage command
        quit.ts              # /quit command
  docs/
    architecture/
    decisions/
    features/
```

This keeps Pi compatibility concerns separate from shell concerns.

---

## UI technology

The shell is built with **OpenTUI** using the **Solid reconciler** (`@opentui/solid`).

OpenTUI provides the terminal rendering primitives the shell requires:

- explicit layout regions (flexbox via Yoga)
- fixed panes
- scroll containers
- focusable inputs (`<input>`, `<textarea>`, `<select>`)
- overlays/dialogs (absolute positioning)
- efficient rerendering under frequent state changes

Renderer-specific assumptions are isolated to the shell layer. The compatibility, backend, and feature layers do not depend on OpenTUI or Solid directly.

---

## Future UI extension support

UI extensions are a future goal, but not part of the first implementation phase.

The architecture should therefore:

- avoid hard-coding app features directly into one monolithic root component
- keep screen, dock, and overlay APIs internally modular
- leave room for feature registration/injection later

But it should **not** prematurely optimize for:

- a public UI plugin API
- compatibility with Pi's extension UI model
- sandboxing and lifecycle guarantees for third-party UI code

Future extension support should be built on the new shell's own abstractions, not inherited from Pi interactive mode.

---

## Migration strategy

## Phase 0 — Architecture and app boundary [done]

- Define compatibility contract
- Define shell model
- Establish app-first source layout

## Phase 1 — Minimal app shell [done]

- standalone entrypoint
- shell layout with fixed dock + scrollable main content
- basic transcript rendering
- basic composer input

## Phase 2 — Pi compatibility baseline [done]

- session loading/saving from the Pi compatibility root (`~/.pi/agent` by default)
- settings loading with precedence:
  - `~/.pi-kit/settings.json`
  - then relevant Pi settings from `~/.pi/agent`
  - then built-in defaults
- storage path conventions
- basic command/runtime bootstrap

## Phase 3 — Feature migration [in progress]

- pager
- wizard
- thread references
- handoff
- ignore-file workflows

## Phase 4 — Product refinement [in progress]

- model/session UX — slash commands for model, thinking, name, switch, sessions:manage [done]
- command palette with filterable picker and native input fields [done]
- reactive session metadata updates via runtime events [done]
- richer overlays
- review flows and other custom affordances

## Phase 5 — Optional extension architecture

- internal extension points first
- public UI extension model later, only if justified

---

## Future refinements

### Mutable working directory within a session

Pi's `SessionManager` treats `cwd` as immutable — it is set once at session creation and stored in the session header. There is no `setCwd()` method, and the session directory path is physically derived from the original cwd.

pi-kit should support changing the working directory during a session. This would require:

- tracking the "current cwd" as app-level state, separate from `session.getCwd()`
- possibly persisting cwd changes as custom session entries so they survive session reload
- updating the shell UI (composer border, footer, file resolution) when cwd changes
- deciding how cwd changes interact with tool execution (e.g., `bash` commands should run in the current cwd, not the session's original cwd)

This is deferred until the core session and agent execution loop are working.

---

## Decision

`v2/` will proceed as a standalone custom coding-agent application with Pi-core compatibility, not as an evolution of the Pi extension-hosted UI.

The app will preserve compatibility with Pi sessions and core behavior where it matters, while replacing the interactive shell entirely with a new architecture built around fixed interaction regions, independently scrollable content, and first-class application-owned screens.

Future UI extension support is a design consideration, but explicitly deferred until the core shell and compatibility layers are stable.
