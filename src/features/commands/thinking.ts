import type { ThinkingLevel } from "@mariozechner/pi-agent-core";
import type { Api, Model } from "@mariozechner/pi-ai";
import type { PaletteContext } from "../../state/palette";
import type { Command } from "./types";

const THINKING_LEVELS: ThinkingLevel[] = [
	"off",
	"minimal",
	"low",
	"medium",
	"high",
];

const THINKING_LEVELS_WITH_XHIGH: ThinkingLevel[] = [
	...THINKING_LEVELS,
	"xhigh",
];

function supportsXhighThinking(model: Model<Api>): boolean {
	return (
		model.id.includes("gpt-5.2") ||
		model.id.includes("gpt-5.3") ||
		model.id.includes("gpt-5.4") ||
		model.id.includes("opus-4-6") ||
		model.id.includes("opus-4.6")
	);
}

function getAvailableThinkingLevels(
	currentModel: Model<Api> | undefined,
): ThinkingLevel[] {
	if (!currentModel?.reasoning) return ["off"];
	return supportsXhighThinking(currentModel)
		? THINKING_LEVELS_WITH_XHIGH
		: THINKING_LEVELS;
}

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
