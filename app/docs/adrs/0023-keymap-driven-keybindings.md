# 0023: Keymap-driven keybindings

## Status

Accepted

## Context

Kit currently handles keyboard shortcuts through scattered `useKeyboard` callbacks, focused input handlers, and component-local key checks. This makes shortcuts harder to standardize, inspect, test, and expose as a user customization surface.

A keybinding customization model should bind keys to stable application commands rather than to implementation-specific event handlers. It should also support layered/focus-aware behavior because Kit has overlapping input surfaces: the composer, pickers, command palette, overlays, review screens, pager, and future plugin-provided UI.

OpenTUI Keymap is a host-agnostic key/command engine for terminal and DOM-like apps. It provides layered bindings, priorities, focus/focus-within scoping, command metadata, multi-key sequences, pending-sequence state, active binding queries, and addons for common binding syntax and behavior. Those capabilities align with Kit's goals for more standard, stable, flexible, and discoverable key handling.

## Decision

Use OpenTUI Keymap as Kit's keybinding and command-dispatch substrate for customizable keyboard shortcuts.

Kit should create one shared OpenTUI keymap for the renderer at app bootstrap and expose it to Solid UI through the OpenTUI Keymap provider.

Customizable shortcuts should be declared as keymap layers with:

- stable command ids
- command metadata such as description and group/category
- default bindings expressed in OpenTUI Keymap syntax
- explicit layer priority/focus semantics when shortcuts overlap
- user overrides loaded from settings

Command ids should use dot-separated lowercase kebab-case segments. Kit-owned commands are unprefixed and use `<domain>.<action>`, for example `command-palette.open`, `composer.submit`, or `picker.move-up`. Unprefixed ids are reserved for Kit core and built-ins.

Plugins should be able to contribute keybindings through the public plugin API, but they should not receive raw access to the app's keymap instance by default. Instead, Kit should expose a capability-oriented registration API that lets plugins declare commands, default bindings, metadata, and lifecycle-scoped disposers. Plugin command ids must start with the plugin id, using `<plugin-id>.<domain>.<action>`, so plugin bindings stay concise while remaining owner-scoped. Kit should reserve its own top-level command domains and reject plugin ids that collide with Kit-owned domains or other registered plugin ids.

User keybinding settings should map command ids to key strings or arrays of key strings. A disabled value should be supported so users can unbind defaults from Kit or plugin commands.

Kit should validate bindings before registering them. Invalid key strings, duplicate bindings, and unsupported overlaps within the same layer should produce warnings and be ignored. When multiple bindings in the same layer collide, Kit should keep the first registered binding and ignore later colliding bindings. Collisions across layers are expected and should use normal layer precedence as the override mechanism rather than being treated as invalid.

Hint bars and shortcut help should derive displayed keys from live keymap state instead of duplicating static labels. A UI surface that declares keymap commands should be able to ask the keymap which active bindings invoke those commands in the current focus/layer context, then render those formatted bindings. Static hint labels are acceptable only as temporary migration scaffolding or for actions not yet represented in the keymap.

Migration should be incremental. Global shell shortcuts and non-text editing commands should move first.

Text input and edit-buffer behavior should remain native by default in the near term. Kit should not re-declare OpenTUI's built-in input editing defaults until there is a clear reason to fully own that behavior. However, user-level key mappings for editor commands should still be possible: Kit can register editor command handlers and only bind user-configured keys, letting unmatched keys fall through to native input handling. A configured editor binding should consume the matching key before the native input handler runs.

This gives users a path to add or remap editor shortcuts without forcing Kit to immediately replace the native OpenTUI input model. Fully disabling unknown/native default editor bindings is out of scope until Kit adopts keymap-managed edit-buffer defaults, because an unbound command cannot prevent a native input component from handling its own default key.

## Consequences

- Key handling becomes command-oriented instead of component-event-oriented.
- Users get a path to customize shortcuts without patching Kit code.
- Stable command ids become part of Kit's user-facing configuration surface and should be renamed cautiously.
- Reserving Kit-owned top-level domains keeps built-in settings concise while plugin-id prefixes prevent extension collisions.
- Plugins can add keyboard-first workflows without depending on shell internals or raw renderer/keymap objects.
- Hint bars stay accurate when defaults change or users customize bindings.
- Future help, which-key, command palette, and debug views can query live keymap state instead of reconstructing shortcut knowledge separately.
- Layer priority and focus scoping become important design concerns for new UI surfaces.
- Binding diagnostics need to distinguish invalid same-layer collisions from intentional cross-layer overrides.
- Kit takes a dependency on the maturity and compatibility of `@opentui/keymap`; the Kit wrapper around it should remain thin to keep upgrades or replacement possible.
- The public plugin API must define validation, collision handling, and cleanup semantics for contributed bindings.
- Native input behavior can remain stable while user-configured editor overrides are layered in selectively.
- Fully disabling native editor defaults may require a later managed edit-buffer migration or an explicit list of native keys to intercept.

## Related

- `docs/adrs/0003-custom-shell.md`
- `docs/adrs/0015-plugin-system.md`
- `docs/adrs/0022-function-plugin-api.md`
- `docs/features/command-palette.md`
- `docs/features/settings.md`
