import { describe, expect, test } from "bun:test";
import {
	listAuthenticatedProviders,
	selectDefaultModel,
} from "./provider-selection";

type TestModel = { id: string };

type TestOptions = {
	providerIds: readonly string[];
	envProviders?: readonly string[];
	modelsByProvider?: Record<string, TestModel[]>;
};

function createOptions(options: TestOptions) {
	const envProviders = new Set(options.envProviders ?? []);
	const modelsByProvider = options.modelsByProvider ?? {};
	return {
		providerIds: options.providerIds,
		hasEnvApiKey: (provider: string) =>
			envProviders.has(provider) ? "present" : undefined,
		getModelsForProvider: (provider: string) =>
			modelsByProvider[provider] ?? [],
	};
}

describe("provider selection", () => {
	test("filters unknown providers and keeps env-backed registered providers", () => {
		const providers = listAuthenticatedProviders(
			["legacy-google", "anthropic", "anthropic", "openai"],
			createOptions({
				providerIds: ["anthropic", "openai", "github-copilot"],
				envProviders: ["github-copilot"],
				modelsByProvider: {
					anthropic: [{ id: "claude-sonnet-4" }],
					openai: [{ id: "gpt-5" }],
					"github-copilot": [{ id: "copilot-gpt-4.1" }],
				},
			}),
		);

		expect(providers).toEqual(["anthropic", "openai", "github-copilot"]);
	});

	test("drops authenticated providers that no longer expose models", () => {
		const providers = listAuthenticatedProviders(
			["retired", "anthropic"],
			createOptions({
				providerIds: ["retired", "anthropic"],
				modelsByProvider: {
					retired: [],
					anthropic: [{ id: "claude-sonnet-4" }],
				},
			}),
		);

		expect(providers).toEqual(["anthropic"]);
	});

	test("prefers the saved model when it is still available", () => {
		const model = selectDefaultModel(
			["anthropic", "openai"],
			"gpt-5",
			createOptions({
				providerIds: ["anthropic", "openai"],
				modelsByProvider: {
					anthropic: [{ id: "claude-sonnet-4" }],
					openai: [{ id: "gpt-5" }],
				},
			}),
		);

		expect(model?.id).toBe("gpt-5");
	});

	test("falls back to the first available model when the saved one disappears", () => {
		const model = selectDefaultModel(
			["retired", "anthropic"],
			"removed-model",
			createOptions({
				providerIds: ["retired", "anthropic"],
				modelsByProvider: {
					retired: [],
					anthropic: [{ id: "claude-sonnet-4" }],
				},
			}),
		);

		expect(model?.id).toBe("claude-sonnet-4");
	});

	test("returns undefined when no authenticated providers remain usable", () => {
		const model = selectDefaultModel(
			["retired"],
			undefined,
			createOptions({
				providerIds: ["retired"],
				modelsByProvider: { retired: [] },
			}),
		);

		expect(model).toBeUndefined();
	});
});
