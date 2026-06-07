import { describe, expect, test } from "bun:test";
import { sanitizeSettings } from "./settings";

describe("sanitizeSettings", () => {
	test("drops removed bell setting", () => {
		expect("bells" in sanitizeSettings({ bells: false })).toBe(false);
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
