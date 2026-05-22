import { describe, expect, test } from "bun:test";
import { getCurrentThemeConfig, resolveAndApplyTheme } from "./theme";

describe("theme config", () => {
	test("returns the current resolved theme config", async () => {
		await resolveAndApplyTheme("system");

		const config = getCurrentThemeConfig();

		expect(config.name).toBe("system");
		expect(config.tokens.textPrimary).toBeString();
		expect(config.tokens).not.toHaveProperty("modalBackdrop");
		expect(config.syntaxPalette.text).toBeString();
	});

	test("returns defensive copies", async () => {
		await resolveAndApplyTheme("system");

		const first = getCurrentThemeConfig();
		first.tokens.textPrimary = "#000000";
		first.syntaxPalette.text = "#000000";

		const second = getCurrentThemeConfig();
		expect(second.tokens.textPrimary).not.toBe("#000000");
		expect(second.syntaxPalette.text).not.toBe("#000000");
	});
});
