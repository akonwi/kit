import { describe, expect, test } from "bun:test";
import { buildOneShotPrompt } from "./one-shot-input";

describe("buildOneShotPrompt", () => {
	test("joins positional prompt words", () => {
		expect(buildOneShotPrompt(undefined, ["review", "this"])).toBe(
			"review this",
		);
	});

	test("separates piped input from the positional prompt", () => {
		expect(buildOneShotPrompt("diff contents", ["review", "this"])).toBe(
			"diff contents\nreview this",
		);
		expect(buildOneShotPrompt("diff contents\n", ["review this"])).toBe(
			"diff contents\nreview this",
		);
	});
});
