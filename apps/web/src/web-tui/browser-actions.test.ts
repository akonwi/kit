import { describe, expect, test } from "bun:test";
import {
	MAX_BROWSER_CLIPBOARD_BYTES,
	parseBrowserBell,
	parseBrowserClipboardWrite,
	parseBrowserNotification,
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

describe("browser notification actions", () => {
	test("parses bounded notification and bell controls", () => {
		expect(
			parseBrowserNotification(
				JSON.stringify({
					type: "notification",
					title: "Kit",
					message: "Agent turn complete",
				}),
			),
		).toEqual({
			type: "notification",
			title: "Kit",
			message: "Agent turn complete",
		});
		expect(parseBrowserBell('{"type":"bell","kind":"error"}')).toEqual({
			type: "bell",
			kind: "error",
		});
	});

	test("rejects malformed and oversized notification controls", () => {
		expect(
			parseBrowserNotification(
				JSON.stringify({ type: "notification", title: "Kit", message: "" }),
			),
		).toBeNull();
		expect(
			parseBrowserNotification(
				JSON.stringify({
					type: "notification",
					title: "Kit",
					message: "x".repeat(501),
				}),
			),
		).toBeNull();
		expect(parseBrowserBell('{"type":"bell","kind":"unknown"}')).toBeNull();
	});
});
