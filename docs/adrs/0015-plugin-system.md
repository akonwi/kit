# 0015: Plugin system

## Status
Proposed

## Context

The current codebase has several problems that a plugin system would solve:

- `AgentRuntime` owns notification config and notification-specific behavior that does not belong in the core runtime
- built-in features such as pager, guided questions, notifications, and session naming are wired ad hoc across app bootstrap and shell layers
- feature-specific dependencies are threaded through command and composer plumbing
- shell components import and render feature-specific overlays directly
- command registration is static rather than dynamic
- built-in features cannot be cleanly enabled, disabled, or isolated

## Decision

Introduce a plugin system with a small app-controlled integration surface.

The initial design centers on:

- a base `Plugin` class
- a `PluginContext`
- a minimal `PluginUI`
- a dynamic `CommandRegistry`
- overlay-driven custom UI via `ui.custom()`

## Core concepts

### Base `Plugin` class

Plugins are classes, not plain objects. The base class owns the constructor shape, default lifecycle hooks, and cleanup helpers.

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

The app owns the UI surface so plugins do not depend on shell internals.

Initial scope:

```ts
interface PluginUI {
  toast(input: {
    title: string;
    subtitle?: string;
    variant: "info" | "warning" | "error";
  }): void;
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
- footer, header, and widget customization APIs

### `CommandRegistry`

The command registry replaces a static command list. Plugins register commands dynamically and the composer reads from the registry.

```ts
interface CommandRegistry {
  register(cmd: Command): () => void;
  getAll(): Command[];
}
```

## Overlay-driven custom UI

`ui.custom()` is backed by an app-owned overlay stack.

The app pushes a component onto the stack, returns a Promise, and resolves that Promise when the component calls `done()`.

This keeps feature-specific modal wiring out of `AppShell` and makes plugin UI flow imperative and app-controlled.

## AgentRuntime slimming direction

The plugin system should move feature-specific behavior out of `AgentRuntime`.

Examples include notification config, notification toggles, and turn-complete notification behavior.

`AgentRuntime` should stay focused on:

- agent lifecycle
- session management
- tool registration
- runtime event emission

## Deferred UI capabilities

These remain out of scope for the first pass:

- `ui.select(...)`
- `ui.input(...)`
- `ui.confirm(...)`
- `ui.setStatus(...)`
- customizable header or footer APIs
- widget or slot APIs around the composer

These should be added only when concrete feature needs exist.

## Rollout order

1. introduce `CommandRegistry`
2. extract notifications from `AgentRuntime`
3. introduce `Plugin`, `PluginContext`, and minimal `PluginUI`
4. add overlay stack support for `ui.custom()`
5. migrate pager to a plugin
6. migrate guided questions to a plugin
7. migrate session naming to a plugin
8. simplify the shell to render generic overlays only

## Consequences

### Positive

- built-in features gain clearer ownership and lifecycle boundaries
- shell code no longer imports feature-specific overlays directly
- command registration becomes dynamic
- runtime code becomes narrower and easier to reason about

### Trade-offs

- plugin infrastructure adds a new architectural layer
- the first pass should keep the surface intentionally small to avoid premature API sprawl

## Related

- `docs/adrs/0003-custom-shell.md`
- `docs/adrs/0022-function-plugin-api.md`
