import { describe, expect, test } from "bun:test";
import { getSupportedThinkingLevels } from "@earendil-works/pi-ai";
import { getModels, getProviders } from "@earendil-works/pi-ai/compat";
import {
	clampThinkingLevel,
	DEFAULT_THINKING_LEVEL,
	getAvailableThinkingLevels,
} from "./thinking-levels";

function findModel(
	predicate: (levels: string[]) => boolean,
): ReturnType<typeof getModels>[number] {
	for (const provider of getProviders()) {
		for (const model of getModels(provider)) {
			const levels = getSupportedThinkingLevels(model);
			if (predicate(levels)) return model;
		}
	}
	throw new Error("No matching model found in pi-ai registry");
}

describe("thinking-levels", () => {
	test("falls back to off when no model is active", () => {
		expect(getAvailableThinkingLevels(undefined)).toEqual(["off"]);
		expect(clampThinkingLevel(undefined, undefined)).toBe("off");
		expect(clampThinkingLevel("high", undefined)).toBe("off");
	});

	test("defaults to medium when the model supports it", () => {
		const model = findModel((levels) =>
			levels.includes(DEFAULT_THINKING_LEVEL),
		);

		expect(clampThinkingLevel(undefined, model)).toBe(DEFAULT_THINKING_LEVEL);
	});

	test("defaults to the highest available level when medium is unavailable", () => {
		const offOnlyModel = findModel(
			(levels) => levels.length === 1 && levels[0] === "off",
		);

		expect(clampThinkingLevel(undefined, offOnlyModel)).toBe("off");
	});

	test("clamps unsupported requests using pi-ai model metadata", () => {
		const noXhighModel = findModel(
			(levels) => levels.includes("high") && !levels.includes("xhigh"),
		);
		const offOnlyModel = findModel(
			(levels) => levels.length === 1 && levels[0] === "off",
		);

		expect(clampThinkingLevel("xhigh", noXhighModel)).toBe("high");
		expect(clampThinkingLevel("medium", offOnlyModel)).toBe("off");
	});
});
