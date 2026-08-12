import { describe, expect, mock, test } from "bun:test";
import type { AgentRuntime } from "../runtime/agent-runtime";
import {
	applyStartupModel,
	isValidModelSelector,
	selectStartupModel,
} from "./headless-model";

const models = [
	{ provider: "openai", id: "gpt-5.5" },
	{ provider: "openrouter", id: "openai/gpt-5.5" },
] as ReturnType<AgentRuntime["getAvailableModels"]>;

describe("headless startup model selection", () => {
	test("validates provider/model-id selectors", () => {
		expect(isValidModelSelector("openai/gpt-5.5")).toBe(true);
		expect(isValidModelSelector("openrouter/openai/gpt-5.5")).toBe(true);
		expect(isValidModelSelector("gpt-5.5")).toBe(false);
		expect(isValidModelSelector("/gpt-5.5")).toBe(false);
		expect(isValidModelSelector("openai/")).toBe(false);
	});

	test("selects an exact provider/model-id pair", () => {
		expect(selectStartupModel(models, "openai/gpt-5.5")).toBe(models[0]);
		expect(selectStartupModel(models, "openrouter/openai/gpt-5.5")).toBe(
			models[1],
		);
	});

	test("reports available models when the selector is unknown", () => {
		expect(() => selectStartupModel(models, "openai/unknown")).toThrow(
			"Available openai models: gpt-5.5",
		);
	});

	test("sets the selected model before waiting for adaptation", async () => {
		const calls: string[] = [];
		const runtime = {
			getAvailableModels: () => models,
			setModel: mock((model: (typeof models)[number]) => {
				calls.push(`set:${model.provider}/${model.id}`);
			}),
			waitForModelAdaptation: mock(async () => {
				calls.push("adapt");
			}),
		};

		await applyStartupModel(runtime as never, "openai/gpt-5.5");

		expect(calls).toEqual(["set:openai/gpt-5.5", "adapt"]);
	});
});
