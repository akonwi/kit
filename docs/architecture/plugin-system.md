# Plugin System

## Status

Proposed. Not yet implemented.

## Motivation

The current codebase has several problems that a plugin system would solve:

- `AgentRuntime` owns notification config and exposes `toggleBells`/`toggleSpeech` — feature-specific logic that doesn't belong in the core runtime
- Built-in features (pager, guided-questions, notifications, session-naming) are wired ad hoc across `bootstrap.tsx` and `App.tsx`
- `ComposerController` carries feature-specific deps (`PagerController`, `GuidedQuestionsController`) only to pass them through to `CommandContext`
- `AppShell` imports and renders feature-specific components (`GuidedQuestionsModal`, `PagerModal`) directly
- The `COMMANDS` array is static — no way to register or disable commands at runtime
- No way to disable built-in features via settings

## Core concepts

### `Plugin`

```ts
interface Plugin {
  id: string;
  setup(ctx: PluginContext): void | (() => void);  // optional teardown
  render?(kit: KitUI): JSX.Element;                // optional UI contribution
}
```

`setup` is called once at app startup. It receives a context with access to the
runtime, command registry, and settings. It may return a teardown function.

`render` is optional. When present, the app calls it as a Solid component and
renders its output. Plugins that only react to events (notifications,
session-naming) don't need it.

### `PluginContext`

```ts
interface PluginContext {
  runtime: AgentRuntime;
  commands: CommandRegistry;
  settings: LoadedSettings;
}
```

### `KitUI` — app-provided component primitives

Passed to `render()` so plugins can use shared UI primitives without importing
from shell internals or re-implementing layout concerns.

```ts
interface KitUI {
  Modal: Component<{
    when: boolean;
    children: JSX.Element;
    bottomInset?: number;
  }>;
  // future: StatusItem, Toast, Banner, ...
}
```

`kit.Modal` is an absolute-positioned, z-indexed overlay with a backdrop.
Plugins use it to show modal UI. It replaces the one-off patterns currently in
`GuidedQuestionsModal` and `PagerModal`.

### `CommandRegistry`

Replaces the static `COMMANDS` array. Plugins register commands at setup time;
the composer controller reads from the registry dynamically.

```ts
interface CommandRegistry {
  register(cmd: Command): () => void;  // returns unregister fn
  getAll(): Command[];
}
```

## What plugins look like

### Pager

```tsx
{
  id: "pager",
  setup({ runtime, commands }) {
    const controller = createPagerController();
    controller.setSubmitCallback(msg => runtime.submitUserMessage(msg));
    runtime.subscribe(event => {
      if (event.type === "turn_complete" && !controller.active)
        controller.tryActivate(runtime.getMessages());
    });
    commands.register(pagerCommand(controller));
    return () => controller.close();
  },
  render(kit) {
    return (
      <kit.Modal when={controller.active}>
        <PagerContent pager={controller} />
      </kit.Modal>
    );
  },
}
```

### Guided questions

```tsx
{
  id: "guided-questions",
  setup({ runtime }) {
    const controller = createGuidedQuestionsController();
    runtime.registerTool(createGuidedQuestionsTool(controller));
  },
  render(kit) {
    return (
      <kit.Modal when={controller.active} bottomInset={dockHeight}>
        <GuidedQuestionsContent guidedQuestions={controller} />
      </kit.Modal>
    );
  },
}
```

### Notifications (no UI)

```ts
{
  id: "notifications",
  setup({ runtime }) {
    return runtime.subscribe(event => {
      if (event.type === "turn_complete") {
        ringBell(...);
        speak(...);
      }
    });
  },
}
```

### Session naming (no UI)

```ts
{
  id: "session-naming",
  setup({ runtime }) {
    return runtime.subscribe(event => {
      if (event.type === "turn_complete")
        maybeAutoNameSession(runtime, runtime.getMessages());
    });
  },
}
```

## How AppShell changes

AppShell no longer imports feature-specific components. It renders plugin output
generically:

```tsx
const kit: KitUI = { Modal };

<box width="100%" height="100%" flexDirection="column">
  <TranscriptPane ... />
  <ComposerDock ... />
  <InlinePicker ... />
  <ToastStack ... />
  <For each={plugins}>{p => p.render?.(kit)}</For>
</box>
```

## How AgentRuntime shrinks

The following leave `AgentRuntime`:

| Removed | Moves to |
|---|---|
| `notificationConfig` field | `notifications` plugin |
| `toggleBells()` / `toggleSpeech()` | bells/speech commands in `notifications` plugin |
| `getNotificationConfig()` | `notifications` plugin internal |
| `notifyTurnComplete()` | `notifications` plugin |
| `notification_config_changed` event | removed from event union |

`AgentRuntime` becomes focused on: agent lifecycle, session management, tool
registration, and event emission (`turns_changed`, `status_changed`,
`turn_complete`, `panel`, `error`, `info`).

## Settings integration

Plugins are opt-out via settings:

```json
// ~/.pi-kit/settings.json
{
  "plugins": {
    "notifications": true,
    "pager": true,
    "guided-questions": true,
    "session-naming": true
  }
}
```

Bootstrap reads this and skips instantiating disabled plugins.

## Rollout order

1. **`CommandRegistry`** — additive, no breakage. Replace static `COMMANDS` array.
2. **Extract notifications** — remove notification logic from `AgentRuntime`, create notifications plugin.
3. **`Plugin` interface + `PluginManager`** — formalize the wiring in bootstrap/App.
4. **`kit.Modal` primitive** — extract shared modal shell from existing components.
5. **Migrate pager → plugin** — remove pager wiring from App.tsx.
6. **Migrate guided-questions → plugin** — remove from bootstrap/App.
7. **Wire session-naming** — finally hook up `auto-name.ts` as a plugin.
8. **Simplify AppShell** — generic plugin render loop, remove feature imports.
