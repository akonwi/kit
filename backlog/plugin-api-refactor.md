# Plugin API refactor

## Summary

Refactor Kit's plugin API to be function-based and capability-based, closer in spirit to Amp and Pi, while preserving Kit's own event model and runtime boundaries.

The immediate goal is to migrate built-in plugins onto the new API. Dynamic loading of user/project plugin files should come later, after built-ins have fully migrated.

## Settled direction

### Public plugin shape

Plugins should be functions that receive a `PluginAPI` object:

```ts
import type { PluginAPI } from "@kit/plugin";

export default function plugin(kit: PluginAPI) {
	kit.on("agent.turn.completed", async (event, ctx) => {
		ctx.ui.toast({
			title: "Turn complete",
			lines: [],
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
		ctx.ui.toast({ title: "Hello", lines: [], variant: "info" });
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

### Compatibility layer

Keep the existing class-based `Plugin` API temporarily as an internal migration adapter for built-ins.

Public/user-facing plugins should only use the function API once dynamic loading is added.

During migration, `PluginManager` can support both shapes internally. Function plugins should use named functions so every plugin has a stable name. Dynamic file loading can later require named default exports or derive names from discovered plugin files before registration.

```ts
type PluginDispose = () => void | Promise<void>;
type PluginInitializer = (
	kit: PluginAPI,
) => void | PluginDispose | Promise<void | PluginDispose>;

type PluginDefinition = PluginClass | PluginInitializer;
```

### Rollout order

1. Introduce `PluginAPI` and function plugin support inside `PluginManager`.
2. Keep current class built-ins loading as-is through the compatibility path.
3. Migrate simple built-ins first:
   - [x] `SessionNamingPlugin`
   - [x] `ClaudeCompatibilityPlugin`
   - [x] `PromptsPlugin`
   - [x] `NotificationsPlugin`
4. Migrate medium/heavier built-ins:
   - `SkillsPlugin`
   - `SubagentsPlugin`
   - `PagerPlugin`
   - `GuidedQuestionsPlugin`
   - `SettingsPlugin`
   - `McpPlugin`
5. Remove or fully internalize the class-based plugin API once built-ins are migrated.
6. Add dynamic loading of user/project plugins as the last step.

### Dynamic plugin loading, later

Defer dynamic user/project plugin loading until after built-ins are migrated.

When implemented, preferred locations are:

- user plugins: `~/.kit/plugins/*.ts`
- project plugins: `.agents/plugins/*.ts`

Initial load order should be:

1. built-ins
2. user plugins
3. project plugins

Project plugins load last because they are most local. Avoid override semantics in v1; command/tool/debug collisions should have explicit behavior, likely fail or skip with a visible warning.

Dynamic imports need cache busting so `/reload` picks up edited plugin files.

## Non-goals for v1

- no marketplace/workspace/global managed plugins
- no separate Amp-compatible event alias layer
- no `kit.onDispose()` helper
- no full tool-call mutation API (`modify`, `synthesize`) unless runtime support is explicitly designed
- no arbitrary stable custom UI surface beyond existing internal/built-in needs

## Related backlog items

- `backlog/plugin-chrome-and-capabilities.md`
- `backlog/tool-call-approvals.md`
