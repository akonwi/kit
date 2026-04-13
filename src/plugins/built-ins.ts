import { GuidedQuestionsPlugin } from "../features/guided-questions";
import { NotificationsPlugin } from "../features/notifications";
import { PagerPlugin } from "../features/pager";
import { SessionNamingPlugin } from "../features/session-naming";
import { SkillsPlugin } from "../features/skills";
import type { PluginClass } from "./PluginManager";

// Built-in plugins that are always enabled as core features.
export const BUILT_IN_PLUGIN_CLASSES: PluginClass[] = [
	SkillsPlugin,
	PagerPlugin,
	GuidedQuestionsPlugin,
	NotificationsPlugin,
	SessionNamingPlugin,
];
