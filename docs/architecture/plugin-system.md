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
ctx.ui.notify("Done!", "info");
await ctx.ui.custom(({ done }) => <PagerContent onClose={done} />);
```

The plugin's async control flow is the UI flow.

For the initial implementation, we should keep the surface intentionally small:
`ui.notify()` and `ui.custom()`. Pi-style helpers like `select`, `input`,
`confirm`, `setStatus`, `setFooter`, and `setWidget` are good future targets,
but should be deferred until we have explicit use cases.

## Core concepts

### Base `Plugin` class

Plugins are classes, not objects. The base class owns the constructor shape,
default no-op lifecycle hooks, and common cleanup helpers.

```ts
abstract class Plugin {
  private readonly disposers: Array<() => void> = [];

  constructor(protected readonly ctx: PluginContext) {}

  async initialize(): Promise<void> {}

  protected subscribeRuntime(
    handler: (event: AgentRuntimeEvent) => void | Promise<void>,
  ): void {
    const unsubscribe = this.ctx.runtime.subscribe((event) => {
      void handler(event);
    });
    this.disposers.push(unsubscribe);
  }

  protected registerCommand(command: Command): void {
    const unregister = this.ctx.commands.register(command);
    this.disposers.push(unregister);
  }

  protected addDisposer(disposer: () => void): void {
    this.disposers.push(disposer);
  }

  async dispose(): Promise<void> {
    for (const disposer of this.disposers.splice(0).reverse()) {
      disposer();
    }
  }
}
```

Notes:

- `ctx` is `protected`, not `private`, so subclasses can use it
- `initialize()` is called immediately after instantiation
- `dispose()` is called during teardown/shutdown
- `subscribeRuntime()` and `registerCommand()` are convenience wrappers that
  automatically register their cleanup behavior
- `addDisposer()` is still available for timers, custom listeners, and anything
  else that needs teardown

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

Initial scope:

```ts
interface PluginUI {
  notify(message: string, type?: "info" | "warning" | "error"): void;
  custom<T>(
    component: (props: { done: (result: T) => void }) => JSX.Element,
  ): Promise<T>;
}
```

Deferred until explicit use cases exist:

- `select(...)`
- `input(...)`
- `confirm(...)`
- `setStatus(...)`
- footer/header/widget customization APIs

### `CommandRegistry`

Replaces the static `COMMANDS` array. Plugins register commands dynamically;
the composer controller reads from the registry.

```ts
interface CommandRegistry {
  register(cmd: Command): () => void;
  getAll(): Command[];
}
```

## What plugins look like

### Pager

```tsx
export class PagerPlugin extends Plugin {
  private readonly pager = createPagerController();

  override async initialize(): Promise<void> {
    this.pager.setSubmitCallback((msg) =>
      this.ctx.runtime.submitUserMessage(msg),
    );

    this.subscribeRuntime(async (event) => {
      if (event.type === "turn_complete") {
        if (this.pager.tryActivate(this.ctx.runtime.getMessages())) {
          await this.openPager();
        }
      }
    });

    this.registerCommand({
      name: "pager",
      description: "Open pager for last assistant response",
      execute: async () => {
        if (!this.pager.tryActivate(this.ctx.runtime.getMessages())) {
          this.ctx.ui.notify("No long response to page through.", "warning");
          return;
        }
        await this.openPager();
      },
    });
  }

  override async dispose(): Promise<void> {
    this.pager.close();
    await super.dispose();
  }

  private async openPager(): Promise<void> {
    await this.ctx.ui.custom<void>(({ done }) => (
      <PagerContent pager={this.pager} onClose={done} />
    ));
  }
}
```

### Guided questions

```tsx
export class GuidedQuestionsPlugin extends Plugin {
  private readonly controller = createGuidedQuestionsController(this.ctx.ui);

  override async initialize(): Promise<void> {
    this.ctx.runtime.registerTool(
      createGuidedQuestionsTool(this.controller),
    );
  }
}
```

The guided-questions controller uses `this.ctx.ui.custom(...)` internally when
the tool is invoked. The tool `await`s that call, so the agent waits for the
user to finish answering before continuing.

### Notifications

```ts
export class NotificationsPlugin extends Plugin {
  override async initialize(): Promise<void> {
    this.subscribeRuntime((event) => {
      if (event.type === "turn_complete") {
        ringBell(...);
        speak(...);
      }
    });
  }
}
```

### Session naming

```ts
export class SessionNamingPlugin extends Plugin {
  override async initialize(): Promise<void> {
    this.subscribeRuntime((event) => {
      if (event.type === "turn_complete") {
        void maybeAutoNameSession(
          this.ctx.runtime,
          this.ctx.runtime.getMessages(),
        );
      }
    });
  }
}
```

## Plugin manager

The plugin manager instantiates plugin classes, calls `initialize()`, and later
calls `dispose()` in reverse order.

```ts
const pluginClasses = [
  NotificationsPlugin,
  PagerPlugin,
  GuidedQuestionsPlugin,
  SessionNamingPlugin,
];

const plugins = pluginClasses.map((PluginClass) => new PluginClass(ctx));

for (const plugin of plugins) {
  await plugin.initialize();
}

// teardown
for (const plugin of plugins.slice().reverse()) {
  await plugin.dispose();
}
```

## How `ui.custom()` works internally

The app maintains a reactive overlay stack. `ui.custom()` pushes a component
onto it and returns a Promise. When `done()` is called, the component is popped
and the Promise resolves.

```ts
const [overlayStack, setOverlayStack] = createSignal<OverlayEntry[]>([]);

const ui: PluginUI = {
  custom<T>(component) {
    return new Promise<T>((resolve) => {
      const id = randomUUID();
      setOverlayStack((prev) => [
        ...prev,
        {
          id,
          render: (done) => component({ done }),
          resolve: (result) => {
            setOverlayStack((prev) => prev.filter((e) => e.id !== id));
            resolve(result as T);
          },
        },
      ]);
    });
  },
};
```

AppShell renders the stack — typically just the top entry:

```tsx
<Show when={overlayStack().length > 0}>
  {overlayStack().at(-1)!.render(overlayStack().at(-1)!.resolve)}
</Show>
```

Because overlays are mounted only when `ui.custom()` is called and unmounted
when `done()` is called, no persistent `active` signals or feature-specific
modal wiring are needed in `AppShell`.

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

`ComposerDock` is locked whenever any overlay is active — no per-feature locked
checks needed.

## How AgentRuntime shrinks

The following leave `AgentRuntime`:

| Removed | Moves to |
|---|---|
| `notificationConfig` field | `NotificationsPlugin` |
| `toggleBells()` / `toggleSpeech()` | notification-related commands in `NotificationsPlugin` |
| `getNotificationConfig()` | `NotificationsPlugin` internal |
| `notifyTurnComplete()` | `NotificationsPlugin` |
| `notification_config_changed` event | removed from event union |

`AgentRuntime` becomes focused on: agent lifecycle, session management, tool
registration, and event emission (`turns_changed`, `status_changed`,
`turn_complete`, `panel`, `error`, `info`).

## Settings integration

Plugins should eventually be opt-out via settings, but we should defer the
exact settings schema until we reach that phase of the refactor. The structure
should be designed together with the actual enable/disable implementation,
not prematurely locked in now.

At that stage we will decide:

- where plugin settings live in `settings.json`
- whether the model is simple booleans or nested plugin-specific config
- whether plugin enable/disable is boot-time only or runtime-reactive

## Deferred UI capabilities

These are explicitly out of scope for the first pass, but worth keeping in mind
for a future Pi-like UI surface:

- `ui.select(...)`
- `ui.input(...)`
- `ui.confirm(...)`
- `ui.setStatus(...)`
- customizable header/footer APIs
- widget/slot APIs above or below the composer

These should be added only when we have concrete feature needs.

## Rollout order

1. **`CommandRegistry`** — additive, no breakage. Replace static `COMMANDS` array.
2. **Extract notifications** — remove notification logic from `AgentRuntime`, create `NotificationsPlugin`.
3. **Introduce `Plugin`, `PluginContext`, and minimal `PluginUI`** — formalize the plugin lifecycle and `ctx.ui` surface with just `notify()` and `custom()`.
4. **Add overlay stack + `ui.custom()`** — centralize modal rendering in the app shell.
5. **Migrate pager → `PagerPlugin`** — replace pager-specific app wiring with `ui.custom()`.
6. **Migrate guided-questions → `GuidedQuestionsPlugin`** — replace guided-questions app wiring with `ui.custom()`.
7. **Wire session-naming → `SessionNamingPlugin`** — finally hook up `auto-name.ts`.
8. **Simplify AppShell** — remove all feature-specific imports, generic overlay stack only.
