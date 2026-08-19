import { describe, expect, test } from "bun:test";
import {
	MAX_BROWSER_CLIPBOARD_BYTES,
	parseBrowserClipboardWrite,
} from "./browser-actions";

describe("parseBrowserClipboardWrite", () => {
	test("accepts a bounded clipboard action", () => {
		expect(
			parseBrowserClipboardWrite(
				JSON.stringify({ type: "clipboard-write", id: 7, text: "**message**" }),
			),
		).toEqual({ type: "clipboard-write", id: 7, text: "**message**" });
	});

	test("rejects malformed and oversized actions", () => {
		expect(parseBrowserClipboardWrite("not json")).toBeNull();
		expect(
			parseBrowserClipboardWrite(
				JSON.stringify({ type: "clipboard-write", id: 0, text: "message" }),
			),
		).toBeNull();
		expect(
			parseBrowserClipboardWrite(
				JSON.stringify({
					type: "clipboard-write",
					id: 1,
					text: "x".repeat(MAX_BROWSER_CLIPBOARD_BYTES + 1),
				}),
			),
		).toBeNull();
	});
});
