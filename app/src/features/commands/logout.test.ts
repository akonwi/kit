import { describe, expect, test } from "bun:test";
import type { Api, Model } from "@earendil-works/pi-ai";
import { authenticatedProviders, reconcileActiveModel } from "./logout";

function fakeModel(id: string, provider: string): Model<Api> {
	return { id, provider } as unknown as Model<Api>;
}

describe("logout provider options", () => {
	test("only includes providers with saved credentials, sorted by name", () => {
		const providers = authenticatedProviders(
			new Set(["openai-codex", "anthropic"]),
		);

		expect(providers.map((provider) => provider.id)).toEqual([
			"anthropic",
			"openai-codex",
		]);
	});

	test("returns nothing when no providers are authenticated", () => {
		expect(authenticatedProviders(new Set())).toEqual([]);
	});

	test("falls back to the raw id for stale/unregistered provider credentials", () => {
		const providers = authenticatedProviders(new Set(["not-a-real-provider"]));

		expect(providers).toEqual([
			{ id: "not-a-real-provider", name: "not-a-real-provider" },
		]);
	});
});

describe("reconcileActiveModel", () => {
	test("does nothing when there is no active model", () => {
		let setModelCalls = 0;
		const runtime = {
			getCurrentModel: () => undefined,
			setModel: () => {
				setModelCalls += 1;
			},
		};

		expect(reconcileActiveModel(runtime, ["anthropic"])).toBeUndefined();
		expect(setModelCalls).toBe(0);
	});

	test("switches to a model from a remaining authenticated provider", () => {
		let setModelArg: unknown;
		const runtime = {
			getCurrentModel: () => fakeModel("gpt-5", "openai"),
			setModel: (model: Model<Api>) => {
				setModelArg = model;
			},
		};

		const fallback = reconcileActiveModel(runtime, ["anthropic"]);

		expect(fallback?.provider).toBe("anthropic");
		expect(setModelArg).toBe(fallback);
	});

	test("leaves the active model untouched when no providers remain authenticated", () => {
		let setModelCalls = 0;
		const runtime = {
			getCurrentModel: () => fakeModel("gpt-5", "openai"),
			setModel: () => {
				setModelCalls += 1;
			},
		};

		expect(reconcileActiveModel(runtime, [])).toBeUndefined();
		expect(setModelCalls).toBe(0);
	});
});
