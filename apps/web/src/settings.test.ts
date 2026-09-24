import { describe, expect, test } from "bun:test";
import { sanitizeSettings } from "./settings";

describe("sanitizeSettings", () => {
	test("drops removed notification settings", () => {
		const settings = sanitizeSettings({
			bells: false,
			speech: { enabled: true, maxChars: 220, voice: "Samantha" },
		});

		expect("bells" in settings).toBe(false);
		expect("speech" in settings).toBe(false);
	});
});

describe("sanitizeSettings default model", () => {
	test("keeps canonical model selectors", () => {
		expect(
			sanitizeSettings({ defaultModel: "  anthropic/claude-sonnet-4-5  " })
				.defaultModel,
		).toBe("anthropic/claude-sonnet-4-5");
	});

	test("drops malformed model selectors", () => {
		expect(
			sanitizeSettings({ defaultModel: "claude-sonnet" }).defaultModel,
		).toBeUndefined();
		expect(
			sanitizeSettings({ defaultModel: "/claude" }).defaultModel,
		).toBeUndefined();
	});
});

describe("sanitizeSettings model overrides", () => {
	test("keeps positive-integer contextWindow keyed by canonical selectors", () => {
		expect(
			sanitizeSettings({
				modelOverrides: {
					"anthropic/claude-sonnet-4-5": { contextWindow: 8192 },
					"  openai/gpt-5  ": { contextWindow: 4096 },
				},
			}).modelOverrides,
		).toEqual({
			"anthropic/claude-sonnet-4-5": { contextWindow: 8192 },
			"openai/gpt-5": { contextWindow: 4096 },
		});
	});

	test("drops non-positive, fractional, and non-numeric contextWindow", () => {
		expect(
			sanitizeSettings({
				modelOverrides: {
					"anthropic/claude-sonnet-4-5": { contextWindow: 0 },
					"openai/gpt-5": { contextWindow: -1 },
					"google/gemini-2.5-pro": { contextWindow: 1.5 },
					"mistral/large": { contextWindow: "8192" },
				},
			}).modelOverrides,
		).toBeUndefined();
	});

	test("drops malformed selectors and non-object overrides", () => {
		expect(
			sanitizeSettings({
				modelOverrides: {
					"claude-sonnet": { contextWindow: 8192 },
					"/claude": { contextWindow: 8192 },
					"anthropic/": { contextWindow: 8192 },
					"anthropic/claude-sonnet-4-5": 8192,
				},
			}).modelOverrides,
		).toBeUndefined();
	});

	test("drops a non-object modelOverrides value", () => {
		expect(
			sanitizeSettings({ modelOverrides: "all" }).modelOverrides,
		).toBeUndefined();
	});
});

describe("sanitizeSettings workspace", () => {
	test("keeps and clamps a finite preferred pane ratio", () => {
		expect(
			sanitizeSettings({ workspace: { paneRatio: 0.55 } }).workspace?.paneRatio,
		).toBe(0.55);
		expect(
			sanitizeSettings({ workspace: { paneRatio: 2 } }).workspace?.paneRatio,
		).toBe(0.9);
	});

	test("drops an invalid preferred pane ratio", () => {
		expect(
			sanitizeSettings({ workspace: { paneRatio: "wide" } }).workspace,
		).toBeUndefined();
	});
});

describe("sanitizeSettings keybindings", () => {
	test("keeps string, array, false, and null keybinding values", () => {
		const settings = sanitizeSettings({
			keybindings: {
				"command-palette.open": "ctrl+p",
				"composer.clear-or-quit": ["ctrl+c", "ctrl+q"],
				"composer.restore-or-recall": false,
				"picker.select": null,
			},
		});

		expect(settings.keybindings).toEqual({
			"command-palette.open": "ctrl+p",
			"composer.clear-or-quit": ["ctrl+c", "ctrl+q"],
			"composer.restore-or-recall": false,
			"picker.select": null,
		});
	});

	test("drops invalid keybinding entries and empty command ids", () => {
		const settings = sanitizeSettings({
			keybindings: {
				"": "ctrl+x",
				"   ": "ctrl+y",
				valid: ["ctrl+a", 1, null, "ctrl+b"],
				invalidObject: { key: "ctrl+o" },
				invalidNumber: 42,
				invalidBoolean: true,
			},
		});

		expect(settings.keybindings).toEqual({
			valid: ["ctrl+a", "ctrl+b"],
		});
	});

	test("omits keybindings when no valid entries remain", () => {
		const settings = sanitizeSettings({
			keybindings: {
				invalidObject: { key: "ctrl+o" },
				invalidNumber: 42,
			},
		});

		expect(settings.keybindings).toBeUndefined();
	});
});
