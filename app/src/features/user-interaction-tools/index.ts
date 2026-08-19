import type { InternalPluginAPI } from "../../plugins";
import { ringBell } from "../notifications/notifications";
import {
	createUserInteractionTools,
	USER_INTERACTION_TOOLS_POLICY,
} from "./tool";

function registerUserInteractionTools(
	kit: InternalPluginAPI,
	notify: () => void,
): void {
	kit.addSystemPrompt(USER_INTERACTION_TOOLS_POLICY);
	for (const tool of createUserInteractionTools({ ui: kit.ui, notify })) {
		kit.registerTool(tool);
	}
}

export function UserInteractionToolsPlugin(kit: InternalPluginAPI): void {
	registerUserInteractionTools(kit, () =>
		ringBell(false, {
			notify: kit.system.notify,
			bell: kit.system.bell,
			title: "Kit",
			message: "Input needed",
		}),
	);
}

export function RemoteUserInteractionToolsPlugin(kit: InternalPluginAPI): void {
	registerUserInteractionTools(kit, () => {});
}
