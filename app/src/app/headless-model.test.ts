import { describe, expect, mock, test } from "bun:test";
import type { AgentRuntime } from "../runtime/agent-runtime";
import {
	applyStartupModel,
	isValidModelSelector,
	resolveStartupModelSelector,
	StartupModelAuthenticationRequiredError,
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

	test("uses configured defaults only for new sessions", () => {
		expect(resolveStartupModelSelector(undefined, "openai/gpt-5.5", true)).toBe(
			"openai/gpt-5.5",
		);
		expect(
			resolveStartupModelSelector(undefined, "openai/gpt-5.5", false),
		).toBeUndefined();
		expect(
			resolveStartupModelSelector(
				"openrouter/openai/gpt-5.5",
				"openai/gpt-5.5",
				true,
			),
		).toBe("openrouter/openai/gpt-5.5");
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

	test("requests authentication when a known startup provider is unavailable", async () => {
		const runtime = {
			getAvailableModels: () =>
				models.filter((model) => model.provider !== "openai"),
			setModel: mock(() => {}),
			waitForModelAdaptation: mock(async () => {}),
		};

		await expect(
			applyStartupModel(runtime as never, "openai/gpt-5.5", {
				isKnown: (provider) => provider === "openai",
				isAuthenticated: () => false,
			}),
		).rejects.toBeInstanceOf(StartupModelAuthenticationRequiredError);
		expect(runtime.setModel).not.toHaveBeenCalled();
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
