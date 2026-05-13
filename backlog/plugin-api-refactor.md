# Plugin API refactor

## Summary

Refactor Kit's plugin API to be function-based and capability-based, closer in spirit to Amp and Pi, while preserving Kit's own event model and runtime boundaries.

The immediate goal is to migrate built-in plugins onto the new API. Dynamic loading of user/project plugin files should come later, after built-ins have fully migrated.

## Settled direction

### Public plugin shape

Plugins should be functions that receive a `PluginAPI` object:

```ts
import type { PluginAPI } from "@akonwi/kit/plugin";

export default function plugin(kit: PluginAPI) {
	kit.on("agent.turn.completed", async (event, ctx) => {
		ctx.ui.toast({
			title: "Turn complete",
			variant: "info",
		});
	});
}
```

Plugin functions may optionally return a dispose function for resources not registered through Kit:

```ts
export default function plugin(kit: PluginAPI) {
	const watcher = startWatcher();

	kit.on("agent.turn.completed", () => {
		// auto-cleaned by PluginManager
	});

	return () => watcher.close();
}
```

Do not add `kit.onDispose()` in v1. Returning a disposer keeps the public API smaller.

### Capability-based API

Do not expose raw internal objects as public plugin API:

- no raw `AgentRuntime`
- no raw `CommandRegistry`
- no raw `AttachmentsController`
- no shell/app state internals

Expose stable capabilities instead. Candidate public surfaces:

- `kit.on(...)`
- `kit.registerCommand(...)`
- `kit.registerTool(...)`
- `kit.addSystemPrompt(...)`
- `kit.addDebugSection(...)`
- `kit.ui.toast(...)`
- session helpers such as `kit.session.get()` and `kit.session.getMessages()`
- settings helpers such as `kit.settings.get()` and `kit.settings.update(...)`
- system helpers such as `kit.system.open(...)`
- scoped logging via `kit.logger.log(...)`

Add wrappers gradually when concrete plugin needs appear.

### Event model

Keep one canonical event set.

Do not introduce separate Amp-style aliases like `tool.call` / `tool.result` if Kit's runtime events are already named differently. Plugin events should use Kit's actual runtime/plugin event names, such as:

- `agent.turn.started`
- `agent.turn.completed`
- `agent.tool.started`
- `agent.tool.updated`
- `agent.tool.ended`
- `session.active.changed`

Tool-call mutation/interception is out of scope for this refactor. Current runtime support is observational tool events plus the existing Pi-core hooks; approval/blocking should be handled by a dedicated future design if needed.

### Command registration shape

Prefer Amp-like command registration over exposing Kit's internal `Command` object directly:

```ts
kit.registerCommand(
	"hello",
	{
		title: "Say hello",
		description: "Show a greeting",
		argName: "name",
	},
	async (ctx) => {
		ctx.ui.toast({ title: "Hello", variant: "info" });
	},
);
```

The manager/API layer maps this into the internal command registry.

### Lifecycle and reload

`/reload` remains a core app command.

Reload should continue to be a hard lifecycle boundary:

1. dispose the plugin manager
2. remove all plugin contributions registered through the API:
   - event listeners
   - commands
   - tools
   - system prompt additions
   - debug sections
   - future chrome/status contributions
3. run any disposer returned by plugin functions
4. reload the runtime/session context
5. reinitialize plugins through fresh `PluginAPI` instances

Registration APIs should auto-register their own cleanup with the manager. Returned disposers are only for plugin-owned external resources.

## Migration strategy

### Internal-only dependencies

The class-based `Plugin` API has been removed. `PluginManager` now manages function plugins only.

Built-ins that need Kit internals should receive them through internal factory closures, not through the public `PluginAPI`. For example, sub-agents need runtime internals to spawn isolated agent runtimes, so the built-in list creates that plugin with an internal factory:

```ts
function createBuiltInPlugins(ctx: PluginContext): PluginDefinition[] {
	return [
		SkillsPlugin,
		createSubagentsPlugin({ runtime: ctx.runtime }),
		PromptsPlugin,
	];
}
```

Public/user-facing plugins should only receive `PluginAPI` once dynamic loading is added. Function plugins should use named functions so every plugin has a stable name. Dynamic file loading can later require named default exports or derive names from discovered plugin files before registration.

```ts
type PluginDispose = () => void | Promise<void>;
type PluginDefinition = (
	kit: PluginAPI,
) => void | PluginDispose | Promise<void | PluginDispose>;
```

### Rollout order

1. Introduce `PluginAPI` and function plugin support inside `PluginManager`.
2. ~~Keep current class built-ins loading as-is through the compatibility path.~~ Done; class plugin support has been removed.
3. Migrate simple built-ins first:
   - [x] `SessionNamingPlugin`
   - [x] `ClaudeCompatibilityPlugin`
   - [x] `PromptsPlugin`
   - [x] `NotificationsPlugin`
4. Migrate medium/heavier built-ins:
   - [x] `SkillsPlugin`
   - [x] `SubagentsPlugin`
     - migrated with an internal factory closure so runtime/sub-agent internals stay out of the public plugin API
   - [x] `PagerPlugin`
   - [x] `GuidedQuestionsPlugin`
   - [x] `SettingsPlugin`
   - [x] `McpPlugin`
5. [x] Remove the class-based plugin API once built-ins are migrated.
6. [x] Add dynamic loading of user/project plugins as the last step.

### API cleanup follow-ups

- [x] Stop exposing `AgentTool` from `@mariozechner/pi-agent-core` in the public plugin API. Kit plugins register tools through Kit-owned tool types and the plugin layer adapts them to Pi internally.

### Dynamic plugin loading

Dynamic user/project plugin loading is implemented for Kit-specific plugin directories.

Plugins are discovered only from Kit-specific locations:

- user plugins: `~/.kit/plugins/*.ts`
- project plugins: `.kit/plugins/*.ts`

Do not use `.agents/plugins/` for Kit plugins. Plugins execute code and are Kit-specific functionality, unlike the compatibility-oriented `.agents/` resources such as skills, prompts, and MCP config.

Load order is:

1. built-ins
2. user plugins
3. project plugins

Project plugins load last because they are most local. Avoid override semantics in v1; command, tool, and debug-section collisions fail the external plugin and are reported through the persistent plugin failure toast.

Dynamic imports need cache busting so `/reload` picks up edited plugin files.

Failed user/project plugins should not be added to `/debug`. Show a persistent toast that requires manual dismissal instead. The toast should summarize failed plugins and include enough file/error detail for the user to fix them.

## Non-goals for v1

- no marketplace/workspace/global managed plugins
- no separate Amp-compatible event alias layer
- no `kit.onDispose()` helper
- no full tool-call mutation API (`modify`, `synthesize`) unless runtime support is explicitly designed
- no arbitrary stable custom UI surface beyond existing internal/built-in needs

## Related backlog items

- `backlog/plugin-chrome-and-capabilities.md`
- `backlog/tool-call-approvals.md`
