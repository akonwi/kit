import type { InternalPluginAPI } from "../../plugins";
import { ringBell } from "../notifications/notifications";
import {
	createUserInteractionTools,
	USER_INTERACTION_TOOLS_POLICY,
} from "./tool";

export function UserInteractionToolsPlugin(kit: InternalPluginAPI): void {
	kit.addSystemPrompt(USER_INTERACTION_TOOLS_POLICY);

	for (const tool of createUserInteractionTools({
		ui: kit.ui,
		notify: () =>
			ringBell(false, {
				notify: kit.system.notify,
				title: "Kit",
				message: "Input needed",
			}),
	})) {
		kit.registerTool(tool);
	}
}
