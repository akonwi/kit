# Plugin System

## Status

Proposed. Not yet implemented.

## Motivation

The current codebase has several problems that a plugin system would solve:

- `AgentRuntime` owns notification config and exposes `toggleBells`/`toggleSpeech` — feature-specific logic that doesn't belong in the core runtime
- Built-in features (pager, guided-questions, notifications, session-naming) are wired ad hoc across `bootstrap.tsx` and `App.tsx`
- `ComposerController` carries feature-specific deps (`PagerController`, `GuidedQuestionsController`) only to thread them into `CommandContext`
- `AppShell` imports and renders feature-specific components (`GuidedQuestionsModal`, `PagerModal`) directly
- The `COMMANDS` array is static — no way to register or disable commands at runtime
- No way to disable built-in features via settings

## Inspiration

Pi's extension system (`ExtensionUIContext`) is the right model. Extensions don't
declare components that the host renders on their behalf. Instead, they call
**imperative async UI operations** through `ctx.ui`:

```ts
// Plugin code reads like normal control flow:
const choice = await ctx.ui.select("Pick a session", options);
const name   = await ctx.ui.input("New name", current);
const ok     = await ctx.ui.confirm("Delete?", "This cannot be undone.");
ctx.ui.notify("Done!", "info");

// Full-screen custom component — blocks until done() is called:
await ctx.ui.custom(({ done }) => <PagerContent onClose={done} />);
```

The plugin's async control flow is the UI flow. No reactive `active` signals,
no persistent mounts, no host-managed overlay registries.

## Core concepts

### `Plugin`

```ts
interface Plugin {
  id: string;
  setup(ctx: PluginContext): void | (() => void);  // optional teardown
}
```

No `render()`. All UI comes through `ctx.ui`.

### `PluginContext`

```ts
interface PluginContext {
  runtime: AgentRuntime;
  commands: CommandRegistry;
  settings: LoadedSettings;
  ui: PluginUI;
}
```

### `PluginUI`

The app-provided UI surface. All methods go through here so the app controls
how each operation is rendered without the plugin needing to know about shell
internals.

```ts
interface PluginUI {
  // Imperative async dialogs — backed by the existing palette/modal/toast infra
  select(title: string, options: string[]): Promise<string | undefined>;
  confirm(title: string, message: string): Promise<boolean>;
  input(title: string, placeholder?: string): Promise<string | undefined>;

  // Fire-and-forget notification — backed by the ToastStack
  notify(message: string, type?: "info" | "warning" | "error"): void;

  // Full-screen overlay — mounts component, resolves when done() is called
  custom<T>(component: (props: { done: (result: T) => void }) => JSX.Element): Promise<T>;

  // Persistent named status item in the status bar
  setStatus(key: string, text: string | undefined): void;

  // future: setFooter(), setWidget(), setHeader()
}
```

### `CommandRegistry`

Replaces the static `COMMANDS` array. Plugins register commands dynamically;
the composer controller reads from the registry.

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
  setup({ runtime, commands, ui }) {
    const controller = createPagerController();
    controller.setSubmitCallback(msg => runtime.submitUserMessage(msg));

    async function openPager() {
      // ui.custom mounts <PagerContent> as a full-screen overlay and
      // resolves when the user closes it. No active signal needed.
      await ui.custom(({ done }) => (
        <PagerContent pager={controller} onClose={done} />
      ));
    }

    runtime.subscribe(event => {
      if (event.type === "turn_complete") {
        if (controller.tryActivate(runtime.getMessages())) openPager();
      }
    });

    commands.register({
      name: "pager",
      description: "Open pager for last assistant response",
      execute() {
        if (!controller.tryActivate(runtime.getMessages())) {
          ui.notify("No long response to page through.", "warning");
          return;
        }
        openPager();
      },
    });
  },
}
```

### Guided questions

```tsx
{
  id: "guided-questions",
  setup({ runtime, ui }) {
    // The controller uses ui.custom internally when the agent invokes the tool.
    const controller = createGuidedQuestionsController(ui);
    runtime.registerTool(createGuidedQuestionsTool(controller));
  },
}
```

`createGuidedQuestionsController` accepts `ui` and calls
`ui.custom(({ done }) => <GuidedQuestionsContent ... onClose={done} />)`
when the agent triggers the tool. The tool `await`s that call, so the agent
waits for the user to finish answering before continuing.

### Notifications (no UI surface needed)

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

## How `ui.custom()` works internally

The app maintains a reactive overlay stack. `ui.custom()` pushes a component
onto it and returns a Promise. When `done()` is called, the component is popped
and the Promise resolves.

```ts
// In the app:
const [overlayStack, setOverlayStack] = createSignal<OverlayEntry[]>([]);

const ui: PluginUI = {
  custom<T>(component) {
    return new Promise<T>(resolve => {
      const id = randomUUID();
      setOverlayStack(prev => [...prev, {
        id,
        render: (done) => component({ done }),
        resolve: (result) => {
          setOverlayStack(prev => prev.filter(e => e.id !== id));
          resolve(result as T);
        },
      }]);
    });
  },
  // ...
};
```

AppShell renders the stack — typically just the top entry:

```tsx
<Show when={overlayStack().length > 0}>
  {overlayStack().at(-1)!.render(overlayStack().at(-1)!.resolve)}
</Show>
```

Because overlays are mounted only when `ui.custom()` is called and unmounted
when `done()` is called, no persistent `when={active()}` props or reactive
`active` signals are needed in the components themselves.

## How AppShell changes

AppShell no longer imports feature-specific components. It holds the overlay
stack and renders the top entry:

```tsx
<box width="100%" height="100%" flexDirection="column">
  <TranscriptPane ... />
  <ComposerDock locked={overlayStack().length > 0} ... />
  <InlinePicker ... />
  <ToastStack ... />
  <Show when={overlayStack().length > 0}>
    {overlayStack().at(-1)!.render(overlayStack().at(-1)!.resolve)}
  </Show>
</box>
```

`ComposerDock` is locked whenever any overlay is active — no per-feature
locked checks needed.

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
3. **`Plugin` + `PluginUI` + overlay stack** — formalize `ctx.ui`, wire into bootstrap/App.
4. **Migrate pager → plugin** — replace PagerModal/pager wiring with `ui.custom()`.
5. **Migrate guided-questions → plugin** — replace GuidedQuestionsModal wiring with `ui.custom()`.
6. **Wire session-naming** — finally hook up `auto-name.ts` as a plugin.
7. **Simplify AppShell** — remove all feature-specific imports, generic overlay stack only.
