import { describe, expect, test } from "bun:test";
import { buildPrintModePrompt } from "./print-mode-input";

describe("buildPrintModePrompt", () => {
	test("joins positional prompt words", () => {
		expect(buildPrintModePrompt(undefined, ["review", "this"])).toBe(
			"review this",
		);
	});

	test("separates piped input from the positional prompt", () => {
		expect(buildPrintModePrompt("diff contents", ["review", "this"])).toBe(
			"diff contents\nreview this",
		);
		expect(buildPrintModePrompt("diff contents\n", ["review this"])).toBe(
			"diff contents\nreview this",
		);
	});
});
