import type {
	InternalPluginAPI,
	InternalPluginDefinition,
} from "../../plugins";
import { ringBell } from "../notifications/notifications";
import type { ScratchpadController } from "./controller";
import { createUpdateScratchpadTool } from "./tool";

export function createScratchpadToolPlugin(options: {
	controller: ScratchpadController;
}): InternalPluginDefinition {
	return function ScratchpadToolPlugin(kit: InternalPluginAPI): () => void {
		const lifecycle = new AbortController();
		kit.registerTool(
			createUpdateScratchpadTool({
				controller: options.controller,
				ui: kit.ui,
				lifecycleSignal: lifecycle.signal,
				notify: () =>
					ringBell(false, {
						notify: kit.system.notify,
						title: "Kit",
						message: "Scratchpad approval needed",
					}),
			}),
		);
		return () => lifecycle.abort();
	};
}
