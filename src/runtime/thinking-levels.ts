import type { ThinkingLevel } from "@earendil-works/pi-agent-core";
import {
	type Api,
	clampThinkingLevel as clampModelThinkingLevel,
	getSupportedThinkingLevels,
	type Model,
} from "@earendil-works/pi-ai";

export const DEFAULT_THINKING_LEVEL: ThinkingLevel = "medium";

export function getAvailableThinkingLevels(
	currentModel: Model<Api> | undefined,
): ThinkingLevel[] {
	if (!currentModel) return ["off"];
	return getSupportedThinkingLevels(currentModel) as ThinkingLevel[];
}

export function clampThinkingLevel(
	level: ThinkingLevel | undefined,
	currentModel: Model<Api> | undefined,
): ThinkingLevel {
	const availableLevels = getAvailableThinkingLevels(currentModel);
	if (!currentModel) return "off";
	if (!level) {
		return availableLevels.includes(DEFAULT_THINKING_LEVEL)
			? DEFAULT_THINKING_LEVEL
			: (availableLevels.at(-1) ?? "off");
	}
	return clampModelThinkingLevel(currentModel, level) as ThinkingLevel;
}
