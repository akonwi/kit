import type { ThinkingLevel } from "@mariozechner/pi-agent-core";
import type { Api, Model } from "@mariozechner/pi-ai";

export const DEFAULT_THINKING_LEVEL: ThinkingLevel = "medium";

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

export function supportsXhighThinking(model: Model<Api>): boolean {
	return (
		model.id.includes("gpt-5.2") ||
		model.id.includes("gpt-5.3") ||
		model.id.includes("gpt-5.4") ||
		model.id.includes("opus-4-6") ||
		model.id.includes("opus-4.6")
	);
}

export function getAvailableThinkingLevels(
	currentModel: Model<Api> | undefined,
): ThinkingLevel[] {
	if (!currentModel?.reasoning) return ["off"];
	return supportsXhighThinking(currentModel)
		? THINKING_LEVELS_WITH_XHIGH
		: THINKING_LEVELS;
}

export function clampThinkingLevel(
	level: ThinkingLevel | undefined,
	currentModel: Model<Api> | undefined,
): ThinkingLevel {
	const availableLevels = getAvailableThinkingLevels(currentModel);
	if (!level) {
		return availableLevels.includes(DEFAULT_THINKING_LEVEL)
			? DEFAULT_THINKING_LEVEL
			: (availableLevels.at(-1) ?? "off");
	}
	return availableLevels.includes(level)
		? level
		: (availableLevels.at(-1) ?? "off");
}
