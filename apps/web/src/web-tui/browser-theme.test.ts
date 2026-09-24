import { describe, expect, test } from "bun:test";
import {
	colorSchemeForBackground,
	parseBrowserThemeMessage,
} from "./browser-theme";

const theme = {
	background: "#fdf6e3",
	foreground: "#123456",
	cursor: "#d33682",
	selectionBackground: "#c8bea4",
	statusBackground: "#eee8d5",
	statusForeground: "#586e75",
	statusBorder: "#b8ad91",
};

describe("browser theme controls", () => {
	test("parses a bounded theme message", () => {
		expect(
			parseBrowserThemeMessage(JSON.stringify({ type: "theme", theme })),
		).toEqual(theme);
	});

	test("rejects malformed and non-hex theme values", () => {
		expect(parseBrowserThemeMessage("not json")).toBeNull();
		expect(
			parseBrowserThemeMessage(
				JSON.stringify({
					type: "theme",
					theme: { ...theme, background: "url(javascript:bad)" },
				}),
			),
		).toBeNull();
		expect(
			parseBrowserThemeMessage(JSON.stringify({ type: "other", theme })),
		).toBeNull();
	});

	test("derives browser color scheme from background luminance", () => {
		expect(colorSchemeForBackground("#0a0a0a")).toBe("dark");
		expect(colorSchemeForBackground("#fdf6e3")).toBe("light");
	});
});
