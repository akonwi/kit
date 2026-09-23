import { describe, expect, test } from "bun:test";
import { kitModels } from "./models";

function modelIds(provider: string): string[] {
	return kitModels.getModels(provider).map((model) => model.id);
}

describe("built-in model catalog", () => {
	test("includes the latest Anthropic and OpenAI releases", () => {
		expect(modelIds("anthropic")).toContain("claude-opus-5-5");
		expect(modelIds("openai")).toEqual(
			expect.arrayContaining(["gpt-6-astra", "gpt-6-luna", "gpt-6-sol"]),
		);
		expect(modelIds("openai-codex")).toEqual(
			expect.arrayContaining(["gpt-6-astra", "gpt-6-luna", "gpt-6-sol"]),
		);
	});
});
