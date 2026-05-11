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
import type { PluginContext, PluginDefinition } from "./types";

// Built-in plugins that are always enabled as core features.
export function createBuiltInPlugins(ctx: PluginContext): PluginDefinition[] {
	return [
		SkillsPlugin,
		createSubagentsPlugin({ runtime: ctx.runtime }),
		PromptsPlugin,
		ClaudeCompatibilityPlugin,
		McpPlugin,
		PagerPlugin,
		GuidedQuestionsPlugin,
		NotificationsPlugin,
		SessionNamingPlugin,
		SettingsPlugin,
	];
}
