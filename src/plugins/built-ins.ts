import { CodeReviewPlugin } from "../features/code-review";
import { GuidedQuestionsPlugin } from "../features/guided-questions";
import { McpPlugin } from "../features/mcp";
import { NotificationsPlugin } from "../features/notifications";
import { PagerPlugin } from "../features/pager";
import { PromptsPlugin } from "../features/prompts";
import { SessionNamingPlugin } from "../features/session-naming";
import { SettingsPlugin } from "../features/settings";
import { SkillsPlugin } from "../features/skills";
import type { PluginClass } from "./PluginManager";

// Built-in plugins that are always enabled as core features.
export const BUILT_IN_PLUGIN_CLASSES: PluginClass[] = [
	SkillsPlugin,
	PromptsPlugin,
	McpPlugin,
	PagerPlugin,
	GuidedQuestionsPlugin,
	NotificationsPlugin,
	SessionNamingPlugin,
	SettingsPlugin,
	CodeReviewPlugin,
];
