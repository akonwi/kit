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

- [ ] Create `src/features/commands/registry.ts`
- [ ] Add `register(command): unregister`
- [ ] Add `getAll(): Command[]`
- [ ] Update `composer-controller.ts` to read commands from the registry
- [ ] Preserve alphabetical sorting in the picker
- [ ] Register today's built-in commands through one path using the registry
- [ ] Remove direct `COMMANDS` array reads from the composer flow
- [ ] Verify slash commands still work

## Phase 2 — Plugin base + PluginManager

- [ ] Create `src/plugins/Plugin.ts`
- [ ] Base class should include:
  - [ ] `constructor(protected readonly ctx: PluginContext)`
  - [ ] `initialize()`
  - [ ] `dispose()`
  - [ ] `subscribeRuntime(...)`
  - [ ] `registerCommand(...)`
  - [ ] `addDisposer(...)`
- [ ] Create `src/plugins/types.ts`
- [ ] Define `PluginContext` with:
  - [ ] `runtime`
  - [ ] `commands`
  - [ ] `settings`
  - [ ] `ui`
- [ ] Create `src/plugins/PluginManager.ts`
- [ ] Instantiate plugin classes and call `initialize()`
- [ ] Dispose plugins in reverse order
- [ ] Keep existing feature wiring intact for now

## Phase 3 — Minimal PluginUI + overlay stack

- [ ] Add app-level overlay stack state
- [ ] Implement `ui.custom<T>(...) => Promise<T>`
- [ ] Render top overlay in `AppShell`
- [ ] Lock composer when overlay stack is non-empty
- [ ] Implement `ui.notify(...)` backed by existing toast system
- [ ] Define cancellation/close semantics for `ui.custom()`
- [ ] Verify focus and cursor behavior while overlays are active

## Phase 4 — PagerPlugin

- [ ] Create `src/plugins/PagerPlugin.ts`
- [ ] Move pager controller ownership into the plugin
- [ ] Register `/pager` via `registerCommand(...)`
- [ ] Subscribe to `turn_complete` via `subscribeRuntime(...)`
- [ ] Replace current pager modal wiring with `ui.custom()`
- [ ] Remove pager-specific props/deps from:
  - [ ] `App.tsx`
  - [ ] `AppShell.tsx`
  - [ ] `ComposerControllerDeps`
  - [ ] `CommandContext`
- [ ] Verify auto-open still works
- [ ] Verify notes submission still works

## Phase 5 — GuidedQuestionsPlugin

- [ ] Create `src/plugins/GuidedQuestionsPlugin.ts`
- [ ] Move guided-questions controller ownership into the plugin
- [ ] Register guided-questions tool from the plugin
- [ ] Replace modal wiring with `ui.custom()`
- [ ] Remove guided-questions-specific props/deps from:
  - [ ] `bootstrap.tsx`
  - [ ] `App.tsx`
  - [ ] `AppShell.tsx`
  - [ ] `ComposerControllerDeps`
  - [ ] `CommandContext`
- [ ] Preserve agent wait-for-user-answer behavior

## Phase 6 — NotificationsPlugin

- [ ] Create `src/plugins/NotificationsPlugin.ts`
- [ ] Move bell/speech turn-complete logic out of `AgentRuntime`
- [ ] Move bell/speech commands into the plugin
- [ ] Keep config storage in a plugin-owned module or reuse current config module
- [ ] Remove from `AgentRuntime`:
  - [ ] `notificationConfig` field
  - [ ] `getNotificationConfig()`
  - [ ] `toggleBells()`
  - [ ] `toggleSpeech()`
  - [ ] `notifyTurnComplete()`
  - [ ] `notification_config_changed` event
- [ ] Verify behavior remains unchanged

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
