# Plugin system refactor

## Goal

Refactor the app toward a class-based plugin architecture with:

- built-in features implemented as plugins
- plugin lifecycle via `initialize()` / `dispose()`
- a minimal Pi-inspired `PluginUI` surface (`notify()` and `custom()` first)
- dynamic command registration
- generic overlay handling in the app shell
- a slimmer `AgentRuntime`

## Constraints / decisions

- Keep the app working after each phase
- Prefer additive migrations over a flag day rewrite
- Defer the exact plugin settings schema until the settings phase
- Initial `PluginUI` scope is only:
  - `ui.notify(...)`
  - `ui.custom(...)`
- Defer Pi-like helpers until we have explicit use cases:
  - `select`
  - `input`
  - `confirm`
  - `setStatus`
  - header/footer/widget APIs

## Phase 1 — CommandRegistry

- [x] Create `src/features/commands/registry.ts`
- [x] Add `register(command): unregister`
- [x] Add `getAll(): Command[]`
- [x] Update `composer-controller.ts` to read commands from the registry
- [x] Preserve alphabetical sorting in the picker
- [x] Register today's built-in commands through one path using the registry
- [x] Remove direct `COMMANDS` array reads from the composer flow
- [x] Verify slash commands still work

## Phase 2 — Plugin base + PluginManager

- [x] Create `src/plugins/Plugin.ts`
- [x] Base class should include:
  - [x] `constructor(protected readonly ctx: PluginContext)`
  - [x] `initialize()`
  - [x] `dispose()`
  - [x] `subscribeRuntime(...)`
  - [x] `registerCommand(...)`
  - [x] `addDisposer(...)`
- [x] Create `src/plugins/types.ts`
- [x] Define `PluginContext` with:
  - [x] `runtime`
  - [x] `commands`
  - [x] `settings`
  - [x] `ui`
- [x] Create `src/plugins/PluginManager.ts`
- [x] Instantiate plugin classes and call `initialize()`
- [x] Dispose plugins in reverse order
- [x] Keep existing feature wiring intact for now

## Phase 3 — Minimal PluginUI + overlay stack

- [x] Add app-level overlay stack state
- [x] Implement `ui.custom<T>(...) => Promise<T>`
- [x] Render top overlay in `AppShell`
- [x] Lock composer when overlay stack is non-empty
- [x] Implement `ui.notify(...)` backed by existing toast system
- [x] Define cancellation/close semantics for `ui.custom()`
- [x] Verify focus and cursor behavior while overlays are active

## Phase 4 — PagerPlugin

- [x] Create `src/features/pager/index.tsx` (consolidated PagerPlugin)
- [x] Move pager controller ownership into the plugin
- [x] Register `/pager` via `registerCommand(...)`
- [x] Subscribe to `turn_complete` via `subscribeRuntime(...)`
- [x] Replace current pager modal wiring with `ui.custom()`
- [x] Remove pager-specific props/deps from:
  - [x] `App.tsx`
  - [x] `AppShell.tsx`
  - [x] `ComposerControllerDeps`
  - [x] `CommandContext`
- [x] Verify auto-open still works
- [x] Verify notes submission still works

## Phase 5 — GuidedQuestionsPlugin

- [x] Create `src/features/guided-questions/index.tsx` (consolidated GuidedQuestionsPlugin)
- [x] Move guided-questions controller ownership into the plugin
- [x] Register guided-questions tool via `runtime.addTool()`
- [x] Replace modal wiring with `ui.custom()`
- [x] Remove guided-questions-specific props/deps from:
  - [x] `bootstrap.tsx`
  - [x] `App.tsx`
  - [x] `AppShell.tsx`
  - [x] `ComposerControllerDeps`
  - [x] `CommandContext`
- [x] Preserve agent wait-for-user-answer behavior
- [x] Add `AgentRuntime.addTool()` for dynamic tool registration

## Phase 6 — NotificationsPlugin

- [x] Create `src/features/notifications/index.tsx` (consolidated NotificationsPlugin)
- [x] Move bell/speech turn-complete logic out of `AgentRuntime`
- [x] Move `/bells` and `/speech` commands into the plugin
- [x] Keep config storage in `notification-config.ts` module
- [x] Remove from `AgentRuntime`:
  - [x] `notificationConfig` field
  - [x] `getNotificationConfig()`
  - [x] `toggleBells()`
  - [x] `toggleSpeech()`
  - [x] `notifyTurnComplete()`
- [x] Add `emitNotificationConfigChanged()` to AgentRuntime for plugin events
- [x] Remove `bellsCommand`/`speechCommand` from built-in commands
- [x] Verify behavior remains unchanged

## Phase 7 — SessionNamingPlugin

- [ ] Create `src/plugins/SessionNamingPlugin.ts`
- [ ] Subscribe to `turn_complete`
- [ ] Call `maybeAutoNameSession(...)`
- [ ] Fix or isolate remaining type issues in `auto-name.ts`
- [ ] Make it easy to disable later through settings

## Phase 8 — Shell/app cleanup

- [ ] Remove feature-specific modal imports from `AppShell`
- [ ] Ensure `AppShell` only knows about:
  - [ ] transcript
  - [ ] composer
  - [ ] picker
  - [ ] toasts
  - [ ] overlay stack
- [ ] Reduce `App.tsx` to composition/bootstrap glue
- [ ] Move remaining feature-specific subscriptions into plugins

## Phase 9 — Settings integration

Do this when the actual plugin migration is far enough along to justify locking
in the settings structure.

- [ ] Design plugin settings structure in `settings.json`
- [ ] Decide whether plugin config is:
  - [ ] simple booleans
  - [ ] nested plugin-specific config
  - [ ] both
- [ ] Decide whether enable/disable is:
  - [ ] boot-time only
  - [ ] runtime-reactive
- [ ] Add default enabled plugin set
- [ ] Gate plugin instantiation by settings

## Deferred / future

Not part of the first refactor pass.

- [ ] `ui.select(...)`
- [ ] `ui.input(...)`
- [ ] `ui.confirm(...)`
- [ ] `ui.setStatus(...)`
- [ ] customizable header/footer APIs
- [ ] widget/slot APIs above or below the composer
- [ ] richer plugin-owned shell surfaces
- [ ] Update `src/runtime/kit-agent.ts` to support unregistering specific tools
      (currently tools are added via `setTools()` which replaces all; need per-tool
      add/remove for dynamic plugin tool lifecycle)

## Definition of done

- [ ] Built-in features are instantiated as plugins
- [ ] Plugins receive runtime events through the base class helpers
- [ ] Plugins invoke app-owned UI through `PluginUI`
- [ ] `AppShell` contains no feature-specific modal wiring
- [ ] `ComposerController` no longer carries feature controllers
- [ ] `AgentRuntime` is focused on core agent/session responsibilities
- [ ] Plugin settings structure is implemented intentionally, not prematurely
