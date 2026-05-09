import { getAvailableThinkingLevels } from "../../runtime/thinking-levels";
import { GLYPH_SUCCESS } from "../../shell/glyphs";
import type { PickerContext } from "../../state/picker";
import type { Command } from "./types";

export const thinkingCommand: Command = {
	name: "thinking",
	description: "Set thinking level",
	execute({ runtime, picker }) {
		const current = runtime.getStatus().thinkingLevel;
		const currentModelId = runtime.getCurrentModelId();
		const currentModel = runtime
			.getAvailableModels()
			.find((model) => model.id === currentModelId);
		const availableLevels = getAvailableThinkingLevels(currentModel);

		picker.show({
			filterable: false,
			hint: "Select a thinking level",
			options: availableLevels.map((level) => ({
				name: level === current ? `${level} ${GLYPH_SUCCESS}` : level,
				description: level === current ? "Current" : "",
				value: level,
				action: (ctx: PickerContext) => {
					runtime.setThinkingLevel(level);
					ctx.dismiss();
				},
			})),
		});
	},
};
