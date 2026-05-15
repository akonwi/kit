import { ClaudeCompatibilityPlugin } from "../features/claude-compat";
import { GuidedQuestionsPlugin } from "../features/guided-questions";
import { McpPlugin } from "../features/mcp";
import { NotificationsPlugin } from "../features/notifications";
import { PagerPlugin } from "../features/pager";
import { PromptsPlugin } from "../features/prompts";
import { SessionNamingPlugin } from "../features/session-naming";
import { SettingsPlugin } from "../features/settings";
import { SkillsPlugin } from "../features/skills";
import { createSubagentsPlugin } from "../features/subagents";
import { UserInteractionToolsPlugin } from "../features/user-interaction-tools";
import type { PluginManagerInput } from "./PluginManager";
import type { InternalPluginDefinition, PluginContext } from "./types";

function internalPlugin(
	initialize: InternalPluginDefinition,
): PluginManagerInput {
	return {
		name: initialize.name,
		initialize,
		internalUi: true,
	};
}

// Built-in plugins that are always enabled as core features.
export function createBuiltInPlugins(ctx: PluginContext): PluginManagerInput[] {
	return [
		internalPlugin(SkillsPlugin),
		internalPlugin(createSubagentsPlugin({ runtime: ctx.runtime })),
		internalPlugin(PromptsPlugin),
		internalPlugin(ClaudeCompatibilityPlugin),
		internalPlugin(McpPlugin),
		internalPlugin(PagerPlugin),
		internalPlugin(GuidedQuestionsPlugin),
		internalPlugin(UserInteractionToolsPlugin),
		internalPlugin(NotificationsPlugin),
		internalPlugin(SessionNamingPlugin),
		internalPlugin(SettingsPlugin),
	];
}
