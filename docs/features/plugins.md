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

A plugin is a TypeScript file with a default function export. Import public SDK types from `@kit/plugin`; do not import from Kit source paths such as `src/plugins`.

```ts
import type { PluginAPI } from "@kit/plugin";

export default function MyPlugin(kit: PluginAPI) {
	kit.registerCommand(
		"hello-plugin",
		{ description: "Show a greeting from a plugin" },
		async (ctx) => {
			ctx.ui.toast({
				title: "Hello plugin",
				lines: ["Loaded from a Kit plugin."],
				variant: "info",
			});
		},
	);
}
```

Plugin functions may return a disposer for resources not registered through Kit:

```ts
import type { PluginAPI } from "@kit/plugin";

export default function WatchPlugin(kit: PluginAPI) {
	const timer = setInterval(() => {
		kit.logger.log("tick");
	}, 1000);

	return () => clearInterval(timer);
}
```

Registrations made through `kit` are cleaned up automatically on `/reload`.

## Reloading

Use `/reload` after editing plugin files. Kit re-discovers plugin files and reloads them with cache busting so changed `.ts` contents are picked up.

Plugin modules are loaded synchronously, so top-level `await` is not supported in plugin files. Async command, event, and tool handlers are supported.

`@kit/plugin` is a type-only SDK surface in v1. Use `import type`; value imports from `@kit/plugin` are not part of the public runtime API.
