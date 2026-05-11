# 0022: Function-based plugin API

## Status
Accepted

## Context

ADR 0015 introduced the plugin system around a base `Plugin` class. During migration, Kit moved built-ins to a function-based API that better matches the intended public plugin shape and avoids exposing internal runtime objects.

One remaining built-in, sub-agents, needs access to Kit internals to spawn and manage isolated agent runtimes. That should not expand the public plugin API.

## Decision

Kit plugins are function initializers:

```ts
type PluginDispose = () => void;
type PluginDefinition = (kit: PluginAPI) => void | PluginDispose;
```

`PluginManager` manages function plugins only. The class-based `Plugin` API is removed.

Built-ins that need internal dependencies use internal factory closures:

```ts
function createBuiltInPlugins(ctx: PluginContext): PluginDefinition[] {
	return [
		SkillsPlugin,
		createSubagentsPlugin({ runtime: ctx.runtime }),
		PromptsPlugin,
	];
}
```

The public `PluginAPI` remains capability-based and does not expose raw `AgentRuntime`, `CommandRegistry`, attachments, shell/app internals, or Pi `AgentTool` types. Plugins register tools with Kit-owned `PluginToolDefinition` / `PluginToolResult` types, and the plugin layer adapts them to Pi internally.

Dynamic user/project plugin loading is deferred until after the built-in class API removal is complete.

## Consequences

- there is one plugin shape in `PluginManager`
- public plugins receive only stable capabilities
- internal built-ins can still depend on Kit internals without making those internals public
- sub-agents stay inside plugin lifecycle/reload cleanup without preserving inheritance

## Related

- `docs/adrs/0015-plugin-system.md`
- `backlog/plugin-api-refactor.md`
