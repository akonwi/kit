import { getAvailableThinkingLevels } from "../../runtime/thinking-levels";
import type { PaletteContext } from "../../state/palette";
import type { Command } from "./types";

export const thinkingCommand: Command = {
	name: "thinking",
	description: "Set thinking level",
	execute({ runtime, palette }) {
		const current = runtime.getStatus().thinkingLevel;
		const currentModelId = runtime.getCurrentModelId();
		const currentModel = runtime
			.getAvailableModels()
			.find((model) => model.id === currentModelId);
		const availableLevels = getAvailableThinkingLevels(currentModel);

		palette.show({
			filterable: false,
			hint: "Select a thinking level",
			options: availableLevels.map((level) => ({
				name: level === current ? `${level} ✓` : level,
				description: level === current ? "Current" : "",
				value: level,
				action: (ctx: PaletteContext) => {
					runtime.setThinkingLevel(level);
					ctx.dismiss();
				},
			})),
		});
	},
};
