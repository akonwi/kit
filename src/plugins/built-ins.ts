import { GuidedQuestionsPlugin } from "../features/guided-questions";
import { NotificationsPlugin } from "../features/notifications";
import { PagerPlugin } from "../features/pager";
import { SessionNamingPlugin } from "../features/session-naming";
import type { PluginClass } from "./PluginManager";

// Built-in plugins that are always enabled as core features.
// Settings only apply to optional/user-installed plugins.
export const BUILT_IN_PLUGIN_CLASSES: PluginClass[] = [
	PagerPlugin,
	GuidedQuestionsPlugin,
	NotificationsPlugin,
	SessionNamingPlugin,
];
