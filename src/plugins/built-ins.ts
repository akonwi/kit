import { GuidedQuestionsPlugin } from "../features/guided-questions";
import { NotificationsPlugin } from "../features/notifications";
import { PagerPlugin } from "../features/pager";
import type { PluginClass } from "./PluginManager";

export const BUILT_IN_PLUGIN_CLASSES: PluginClass[] = [
	PagerPlugin,
	GuidedQuestionsPlugin,
	NotificationsPlugin,
];
