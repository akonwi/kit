# Plugins

Kit can load trusted TypeScript plugins that extend the app through the public `PluginAPI` capability surface.

## Locations

Kit loads plugins from Kit-specific directories only:

1. user plugins: `~/.kit/plugins/*.ts`
2. project plugins: `.kit/plugins/*.ts`

Discovery is non-recursive. Only direct `.ts` files in those directories are loaded.

Project plugins load after user plugins. Built-in plugins load before both.

If an external plugin registers a command, tool, or debug section that already exists, Kit treats that as a plugin failure and reports it with a persistent toast.

Kit does **not** load plugins from `.agents/plugins/`. Plugins execute code and are Kit-specific functionality, while `.agents/` is reserved for compatibility-oriented resources such as prompts, skills, and MCP config.

## Trust model

Plugins execute local code in the Kit process. Only use plugins from people and projects you trust.

A failed user/project plugin does not stop Kit from starting. Kit shows a persistent toast with the plugin file and error. Dismiss it manually after reviewing the failure.

## Writing a plugin

A plugin is a TypeScript file with a default function export. Import public SDK types from `@akonwi/kit/plugin`; do not import from Kit source paths such as `src/plugins`.

```ts
import type { PluginAPI } from "@akonwi/kit/plugin";

export default function MyPlugin(kit: PluginAPI) {
	kit.registerCommand(
		"hello-plugin",
		{ description: "Show a greeting from a plugin" },
		async (ctx) => {
			ctx.ui.toast({
				title: "Hello plugin",
				subtitle: "Loaded from a Kit plugin.",
				variant: "info",
			});
		},
	);
}
```

Plugin functions may return a `Disposer` for resources not registered through Kit:

```ts
import type { PluginAPI } from "@akonwi/kit/plugin";

export default function WatchPlugin(kit: PluginAPI) {
	const timer = setInterval(() => {
		kit.logger.log("tick");
	}, 1000);

	return () => clearInterval(timer);
}
```

Registrations made through `kit` are cleaned up automatically on `/reload`.

Common exported SDK types include `PluginAPI`, `Plugin`, `Disposer`, `CommandContext`, `CommandOptions`, `RuntimeEvent`, `EventContext`, `ToolDefinition`, `ToolResult`, `ToolCall`, and `ToolCallDecision`.

## UI helpers

`kit.ui.toast({ title, subtitle, variant })` remains the lightweight notification API. For small interactive flows, Kit also provides app-owned UI primitives:

```ts
const picked = await kit.ui.select({
	title: "Choose target",
	options: [
		{ label: "Current file", value: "file", description: "Use the active context" },
		{ label: "Whole project", value: "project" },
	],
	filterable: true,
});

const name = await kit.ui.input({
	title: "Name this run",
	placeholder: "experiment name",
});

const ok = await kit.ui.confirm({
	title: "Continue?",
	message: "This will submit a follow-up message.",
	confirmLabel: "Continue",
});
```

These helpers use Kit-owned dialogs and return `undefined` when selection/input is cancelled. `confirm` returns `false` for cancel/escape. The public plugin UI API is intentionally limited to `toast`, `select`, `input`, and `confirm` so Kit can keep ownership of rendering, focus, theme, and compatibility.

## Tool approval hooks

Plugins can register a callback that runs before a tool executes. Return `{ action: "allow" }` or no value to run the tool; return `{ action: "reject-and-continue", message }` to block it and let the agent continue.

If multiple plugins register tool-call handlers, Kit evaluates them in registration order. `allow` does not short-circuit; the first rejection blocks the call.

```ts
kit.onToolCall(async (toolCall, ctx) => {
	if (toolCall.name !== "bash") return { action: "allow" };
	const command = toolCall.input.command;
	if (typeof command !== "string" || !command.includes("rm")) {
		return { action: "allow" };
	}

	const approved = await ctx.ui.confirm({
		title: "Approve bash?",
		message: command,
		confirmLabel: "Allow",
		cancelLabel: "Block",
		defaultValue: false,
	});

	return approved
		? { action: "allow" }
		: { action: "reject-and-continue", message: "User denied bash." };
});
```

## Reloading

Use `/reload` after editing plugin files. Kit re-discovers plugin files and reloads them with cache busting so changed `.ts` contents are picked up.

Plugin modules are loaded synchronously, so top-level `await` is not supported in plugin files. Async command, event, and tool handlers are supported.

`@akonwi/kit/plugin` is a type-only SDK surface in v1. Use `import type`; value imports from `@akonwi/kit/plugin` are not part of the public runtime API.
