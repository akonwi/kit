import { ClaudeCompatibilityPlugin } from "../features/claude-compat";
import { GuidedQuestionsPlugin } from "../features/guided-questions";
import { createMcpPlugin } from "../features/mcp";
import { NotificationsPlugin } from "../features/notifications";
import { PagerPlugin } from "../features/pager";
import { PromptsPlugin } from "../features/prompts";
import { SessionCwdPlugin } from "../features/session-cwd";
import { SessionNamingPlugin } from "../features/session-naming";
import { SettingsPlugin } from "../features/settings";
import { SkillsPlugin } from "../features/skills";
import type {
	SubagentParentStorage,
	SubagentSessionStorage,
} from "../features/subagents";
import { createSubagentsPlugin } from "../features/subagents";
import { UserInteractionToolsPlugin } from "../features/user-interaction-tools";
import { VcsStatusPlugin } from "../features/vcs/plugin";
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

export type BuiltInPluginOptions = {
	headless?: boolean;
	onReady?: (ready: Promise<void>) => void;
	subagentParentStorage?: SubagentParentStorage;
	subagentStorage?: SubagentSessionStorage;
};

// Built-in plugins that are always enabled as core features.
export function createBuiltInPlugins(
	ctx: PluginContext,
	options: BuiltInPluginOptions = {},
): PluginManagerInput[] {
	return [
		internalPlugin(SkillsPlugin),
		internalPlugin(
			createSubagentsPlugin({
				runtime: ctx.runtime,
				onReady: options.onReady,
				parentStorage: options.subagentParentStorage,
				subagentStorage: options.subagentStorage,
			}),
		),
		...(options.headless ? [] : [internalPlugin(PromptsPlugin)]),
		internalPlugin(ClaudeCompatibilityPlugin),
		internalPlugin(
			createMcpPlugin({
				interactive: !options.headless,
				onReady: options.onReady,
				persistState: !options.headless,
			}),
		),
		...(options.headless ? [] : [internalPlugin(VcsStatusPlugin)]),
		internalPlugin(SessionCwdPlugin),
		...(options.headless
			? []
			: [
					internalPlugin(PagerPlugin),
					internalPlugin(GuidedQuestionsPlugin),
					internalPlugin(UserInteractionToolsPlugin),
					internalPlugin(NotificationsPlugin),
					internalPlugin(SessionNamingPlugin),
					internalPlugin(SettingsPlugin),
				]),
	];
}
