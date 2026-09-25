import { describe, expect, test } from "bun:test";
import { formatTerminalTitle } from "./terminal-title";

describe("terminal title", () => {
	test("marks active turns without losing session context", () => {
		expect(formatTerminalTitle("Fix pager", "/work/kit", true)).toBe(
			"⏳ kit - Fix pager - kit",
		);
		expect(formatTerminalTitle(undefined, "/work/kit", true)).toBe(
			"⏳ kit - kit",
		);
	});

	test("uses the existing idle title format", () => {
		expect(formatTerminalTitle("Fix pager", "/work/kit")).toBe(
			"kit - Fix pager - kit",
		);
	});
});
