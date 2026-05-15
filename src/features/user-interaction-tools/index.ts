import type { InternalPluginAPI } from "../../plugins";
import {
	createUserInteractionTools,
	USER_INTERACTION_TOOLS_POLICY,
} from "./tool";

export function UserInteractionToolsPlugin(kit: InternalPluginAPI): void {
	kit.addSystemPrompt(USER_INTERACTION_TOOLS_POLICY);

	for (const tool of createUserInteractionTools({
		ui: kit.ui,
		getSettings: () => kit.settings.get(),
	})) {
		kit.registerTool(tool);
	}
}
