# Architecture Feedback

## What's working well

### 1. Clean layer separation
The `compat/ → backend/ → state/ → features/ → shell/` stack has clear directional dependencies. No layer reaches down inappropriately. The `compat/` layer is thin and focused — paths, settings, sessions — and the `backend/` wraps `pi-coding-agent` behind a clean event-driven facade.

### 2. Controller pattern
The `ComposerController` and `PagerController` both follow the same shape: a factory function that returns a plain object with reactive state + methods. This keeps behavioral logic testable without UI and lets components stay thin. Good pattern — worth making explicit as a project convention.

### 3. `AgentRuntime` as the single backend boundary
Everything goes through one abstraction. Commands, the composer, the pager — they all talk to `AgentRuntime`, not to `AgentSession` or `SessionManager` directly. This makes it feasible to swap the backend or mock it for tests.

### 4. Palette as a generic primitive
After extracting `showCommands` into the composer controller, `PaletteManager` is truly generic — stack-based, supports list + input modes, filterable, key-bindable. Commands like `/model`, `/switch`, and `/sessions:manage` compose it naturally without special-casing.

---

## Areas for improvement

### 1. `createAppState` does too many things
It creates the Solid store, subscribes to runtime events, creates the `PaletteManager`, creates the `FileIndex`, creates the `ThreadIndex`, and manages the file-index invalidation counter. That's state management, event wiring, and service instantiation all in one function.

Consider splitting it:
- `createAppStore()` — just the store + runtime subscription (pure state sync)
- Move `FileIndex` and `ThreadIndex` creation into `App.tsx` or a dedicated `createServices()` function
- The palette already moved to the composer controller, so it could be removed from `createAppState` entirely (it's still exported from there though)

### 2. `PaletteManager` ownership is ambiguous
Currently `createAppState` creates it, but `createComposerController` *also* creates one. Looking at `App.tsx`, the controller's palette is what gets used. So the one in `app-state` is dead code — or there's a mismatch. This should be clarified: either the palette lives in the controller (since it's the only consumer), or it's created once at the app level and injected into the controller.

### 3. `CommandContext` couples commands to `PaletteManager`
Every command receives `{ runtime, palette, pager }`. Commands like `/new` and `/quit` don't use the palette at all. Commands like `/model` and `/switch` use it heavily. This is a wide interface that forces all commands to know about all dependencies.

Options:
- Accept the wide context as pragmatic (it's a small set of commands)
- Or narrow it: commands that need a picker could receive a `showPicker` callback instead of the full palette

Not urgent, but worth noting if the command set grows.

### 4. `TranscriptPane` is doing a lot of message parsing inline
`extractAssistantParts`, `extractToolResultLines`, `extractUserText`, `buildToolResultMap` — these are all domain logic for interpreting Pi's message format, but they live in a UI component file. If you ever need to render messages elsewhere (e.g. in the pager, or in tests), you'd need to duplicate or extract them.

Suggest: move message-parsing helpers to `compat/messages.ts` or `features/messages.ts`. The component just receives pre-shaped data or calls into shared helpers.

### 5. `expand-references.ts` bypasses the runtime
It calls `SessionManager.open()` and `SessionManager.listAll()` directly — the only code outside `agent-runtime.ts` that touches `pi-coding-agent` internals. This breaks the "everything goes through `AgentRuntime`" principle.

The runtime already has `listAllSessions()`. It could also expose a `readSessionMessages(path)` method so the reference expander doesn't need to import `SessionManager`.

### 6. No error surface for the user
The runtime emits `{ type: "error" }` events, but nothing in the UI subscribes to them or renders them. Errors from commands, tool failures, and thread reference issues all go to `console.error`. Users see nothing. This is fine for now but will bite you quickly — especially for things like failed model switches or bad thread references.

### 7. Pager ↔ Composer coupling is implicit
The pager is created in `App.tsx`, passed to the composer controller, and the composer changes behavior when `pager.active` is true (suppresses slash commands, ignores Enter). But this coupling is scattered — you have to read the composer controller carefully to understand it. A comment or a more explicit "mode" concept (normal vs pager) on the controller would help.

### 8. No tests
The codebase has good seams for testing — controllers are plain functions, the palette is fully in-memory, the file/thread indexes are mockable. But there appear to be no tests yet. The split-sections logic, expand-references, scoring, and the composer trigger detection are all good candidates for unit tests.

---

## Minor observations

- `filePickerActive` and `threadPickerActive` in the composer controller are set but never read. Dead state — can be removed.
- The `onFilterChange` callback in `PaletteConfig` (used for the `@` → `@@` transition in the file picker) is a clever hack but fragile. It's doing text manipulation on the textarea from inside a palette dismiss callback. Worth a comment explaining the intent.
- `theme.ts` centralizes colors well. If you ever want user-customizable themes, the token-based approach will make that straightforward.
- The `features/commands/pager.ts` file exists but wasn't imported in the commands index — worth checking if it's wired up or orphaned.

---

## Summary

| Strength | Risk |
|---|---|
| Clean layer boundaries | `createAppState` is a grab-bag |
| Controller pattern for behavior | Palette ownership is ambiguous |
| Single runtime abstraction | `expand-references` bypasses it |
| Generic palette primitive | No error UI for users |
| Good seams for testability | No tests yet |

The architecture is solid for the stage it's at. The highest-leverage improvements would be: **(1)** clarify palette ownership, **(2)** extract message-parsing from `TranscriptPane`, and **(3)** add a minimal error display. Everything else is refinement.
