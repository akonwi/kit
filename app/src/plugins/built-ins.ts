import { ClaudeCompatibilityPlugin } from "../features/claude-compat";
import { GuidedQuestionsPlugin } from "../features/guided-questions";
import { createMcpPlugin } from "../features/mcp";
import { NotificationsPlugin } from "../features/notifications";
import { PagerPlugin } from "../features/pager";
import { PromptsPlugin } from "../features/prompts";
import {
	createReleasesPlugin,
	type ReleasesWorkspaceController,
} from "../features/releases";
import { createScratchpadToolPlugin } from "../features/scratchpad";
import type { ScratchpadController } from "../features/scratchpad/controller";
import { SessionCwdPlugin } from "../features/session-cwd";
import { SessionNamingPlugin } from "../features/session-naming";
import { SettingsPlugin } from "../features/settings";
import { SkillsPlugin } from "../features/skills";
import type {
	SubagentParentStorage,
	SubagentSessionStorage,
	SubagentsWorkspaceController,
} from "../features/subagents";
import { createSubagentsPlugin } from "../features/subagents";
import { UserInteractionToolsPlugin } from "../features/user-interaction-tools";
import { VcsStatusPlugin } from "../features/vcs/plugin";
import type { PluginManagerInput } from "./PluginManager";
import type { InternalPluginDefinition, PluginContext } from "./types";

function internalPlugin(
	initialize: InternalPluginDefinition,
	options: { chromePrefix?: string } = {},
): PluginManagerInput {
	return {
		name: initialize.name,
		chromePrefix: options.chromePrefix,
		initialize,
		internalUi: true,
	};
}

export type BuiltInPluginOptions = {
	headless?: boolean;
	onReady?: (ready: Promise<void>) => void;
	subagentParentStorage?: SubagentParentStorage;
	subagentStorage?: SubagentSessionStorage;
	subagentsWorkspace?: SubagentsWorkspaceController;
	releasesWorkspace?: ReleasesWorkspaceController;
	scratchpad?: ScratchpadController;
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
				workspace: options.subagentsWorkspace,
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
		...(options.headless
			? []
			: [
					internalPlugin(VcsStatusPlugin, { chromePrefix: "kit.footer" }),
					...(options.releasesWorkspace
						? [
								internalPlugin(
									createReleasesPlugin({
										workspace: options.releasesWorkspace,
									}),
									{ chromePrefix: "kit.header" },
								),
							]
						: []),
				]),
		internalPlugin(SessionCwdPlugin),
		...(options.headless
			? []
			: [
					...(options.scratchpad
						? [
								internalPlugin(
									createScratchpadToolPlugin({
										controller: options.scratchpad,
									}),
								),
							]
						: []),
					internalPlugin(PagerPlugin),
					internalPlugin(GuidedQuestionsPlugin),
					internalPlugin(UserInteractionToolsPlugin),
					internalPlugin(NotificationsPlugin),
					internalPlugin(SessionNamingPlugin),
					internalPlugin(SettingsPlugin),
				]),
	];
}
