# Plugins

Kit can load trusted TypeScript plugins that extend the app through the public `PluginAPI` capability surface.

## Locations

Kit loads plugins from Kit-specific directories only:

1. user plugins: `~/.kit/plugins/*.ts`
2. project plugins: `.kit/plugins/*.ts`

Discovery is non-recursive. Only direct `.ts` files in those directories are loaded.

Project plugins load after user plugins. Built-in plugins load before both.

Built-in plugins initialize during core app setup. User and project plugins load in the background after the shell is ready, so slow dependency installation or bundling does not block basic startup. Commands, tools, and chrome contributions from those external plugins become available once loading finishes.

If an external plugin registers a command, tool, or debug section that already exists, Kit treats that as a plugin failure and reports it with a persistent toast.

Kit does **not** load plugins from `.agents/plugins/`. Plugins execute code and are Kit-specific functionality, while `.agents/` is reserved for compatibility-oriented resources such as prompts, skills, and MCP config.

## Plugin dependencies

Plugin directories may have their own `package.json`, lockfile, and `node_modules`:

```text
~/.kit/plugins/
  package.json
  bun.lock
  node_modules/
  my-plugin.ts

project/.kit/plugins/
  package.json
  bun.lock
  node_modules/
  project-plugin.ts
```

Kit automatically installs dependencies for each plugin directory before bundling. It uses `bun install` when the `bun` CLI is available and falls back to `npm install` otherwise.

Kit bundles each plugin from its absolute file path before loading it, so package imports resolve from the plugin file's directory and then walk up through normal Bun/Node module resolution. This lets user and project plugins depend on packages that Kit itself does not ship.

## Trust model

Plugins execute local code in the Kit process. Only use plugins from people and projects you trust.

A failed user/project plugin does not stop Kit from starting. Kit shows a persistent toast with the plugin file and error. Dismiss it manually after reviewing the failure.

## Writing a plugin

A plugin is a TypeScript file with a default function export. Import public SDK types from `@akonwi/kit/plugin`; do not import from Kit source paths such as `src/plugins`.

```ts
import { Type, type PluginAPI } from "@akonwi/kit/plugin";

export default function MyPlugin(kit: PluginAPI) {
	kit.registerCommand(
		"hello-world",
		{ description: "Show a greeting from a plugin" },
		async (ctx) => {
			ctx.ui.toast({
				title: "Hello plugin",
				subtitle: "Loaded from a Kit plugin.",
				variant: "info",
			});
		},
	);

	kit.registerTool({
		name: "echo_plugin",
		description: "Echo text from a plugin tool.",
		parameters: Type.Object({ text: Type.String() }),
		async execute(_id, params) {
			return {
				content: [{ type: "text", text: params.text }],
				details: {},
			};
		},
	});
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

Plugin command ids are also keybinding ids. Users can bind a plugin command in `~/.kit/settings.json` after it loads:

```json
{
  "keybindings": {
    "hello-world": "ctrl+h"
  }
}
```

Choose command ids that start with your plugin name or another owned namespace to avoid conflicts, for example `MyPlugin.open` or `github.openPullRequest`.

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

These helpers use Kit-owned dialogs and return `undefined` when selection/input is cancelled. `confirm` returns `false` for cancel/escape. The public plugin UI API is intentionally limited to `toast`, `select`, `input`, `confirm`, and the `text`/`theme` helpers so Kit can keep ownership of rendering, focus, theme, and compatibility.

## Header and footer status contributions

Plugins can contribute short text items to the header and bottom footer. Kit owns the rendering and layout; plugins provide text or styled text chunks.

```ts
kit.footer.set("build", "build: passing", { side: "right" });
kit.footer.set("mode", "watching", { side: "left" });
kit.header.set("branch", "main", { side: "right" });

const theme = kit.ui.theme();

kit.footer.set(
	"ci",
	[
		kit.ui.text("✓", { fg: theme.tokens.toolText, bold: true }),
		" tests ",
		kit.ui.text("passing", { fg: theme.tokens.toolText }),
	],
	{
		side: "right",
		onClick: () => kit.system.open("https://github.com/org/repo/actions"),
	},
);

// Clear an item
kit.footer.clear("build");
kit.header.clear("branch");

// Hide a known item contributed by another plugin or built-in.
// The disposer restores it.
const showDefaultLocation = kit.footer.hide("VcsStatusPlugin:location");
const showDefaultModel = kit.header.hide("HeaderBar:model");
showDefaultLocation();
showDefaultModel();
```

Header/footer item IDs passed to `set`/`clear` are scoped to the plugin and are cleaned up automatically when the plugin is disposed or reloaded. `hide` accepts the full item ID to support replacing known built-in contributions.

Use `kit.ui.text(text, style)` to style part or all of a contribution. Supported style fields are `fg`, `bg`, `bold`, `dim`, `italic`, `underline`, and `strikethrough`. Use `kit.ui.theme()` when setting or updating contributions to read the current resolved theme config (`name`, `tokens`, and `syntaxPalette`) and blend with Kit's colors. `onClick` is a whole-contribution action; Kit maps it to terminal mouse events and does not expose raw mouse events to plugins.

Built-in header item IDs are `HeaderBar:title`, `HeaderBar:model`, `HeaderBar:bell`, and `HeaderBar:speech`.

Built-in internal plugins may use additional app-owned capabilities that are not part of the public plugin SDK. For example, built-ins can read VCS state while the public SDK only exposes chrome contribution rendering.

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

Use `/reload` after editing plugin files. Kit re-discovers plugin files, re-bundles them into Kit's plugin cache, and imports the fresh bundles so changed `.ts` contents are picked up. External plugin loading continues in the background after the session reload completes.

Plugin modules are loaded synchronously, so top-level `await` is not supported in plugin files. Async command, event, and tool handlers are supported.

`@akonwi/kit/plugin` exports plugin API types and the runtime `Type` schema helper. Use `import type` for types such as `PluginAPI`, and use the value import `Type` when defining tool parameter schemas.
