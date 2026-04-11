import { GuidedQuestionsPlugin } from "../features/guided-questions";
import { PagerPlugin } from "../features/pager";
import type { PluginClass } from "./PluginManager";

// Phase 4: pager migrated to plugin.
// Phase 5: guided-questions migrated to plugin.
export const BUILT_IN_PLUGIN_CLASSES: PluginClass[] = [
	PagerPlugin,
	GuidedQuestionsPlugin,
];
