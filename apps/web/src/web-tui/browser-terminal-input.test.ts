import { describe, expect, test } from "bun:test";
import {
	type BrowserKeyLike,
	classifyBrowserPlatform,
	encodeBrowserKey,
	TerminalProtocolState,
} from "./browser-terminal-input";

function key(
	value: string,
	overrides: Partial<BrowserKeyLike> = {},
): BrowserKeyLike {
	return {
		key: value,
		ctrlKey: false,
		shiftKey: false,
		altKey: false,
		metaKey: false,
		...overrides,
	};
}

describe("encodeBrowserKey", () => {
	test("normalizes the complete control-character matrix", () => {
		expect(encodeBrowserKey(key("Escape"))).toBe("\x1b");
		for (let code = 1; code <= 26; code += 1) {
			const letter = String.fromCharCode(96 + code);
			if (letter === "v") continue;
			expect(encodeBrowserKey(key(letter, { ctrlKey: true }))).toBe(
				String.fromCharCode(code),
			);
		}
		for (const [value, expected] of [
			[" ", "\x00"],
			["[", "\x1b"],
			["\\", "\x1c"],
			["]", "\x1d"],
			["^", "\x1e"],
			["_", "\x1f"],
			["?", "\x7f"],
		] as const) {
			expect(encodeBrowserKey(key(value, { ctrlKey: true }))).toBe(expected);
		}
	});

	test("encodes fixed and modified navigation keys", () => {
		for (const [value, expected] of [
			["Enter", "\r"],
			["Backspace", "\x7f"],
			["Tab", "\t"],
			["Insert", "\x1b[2~"],
			["Delete", "\x1b[3~"],
			["PageUp", "\x1b[5~"],
			["PageDown", "\x1b[6~"],
			["F1", "\x1bOP"],
			["F12", "\x1b[24~"],
		] as const) {
			expect(encodeBrowserKey(key(value))).toBe(expected);
		}
		expect(encodeBrowserKey(key("ArrowUp"))).toBe("\x1b[A");
		expect(encodeBrowserKey(key("ArrowRight", { altKey: true }))).toBe(
			"\x1b[1;3C",
		);
		expect(encodeBrowserKey(key("Home", { ctrlKey: true }))).toBe("\x1b[1;5H");
		expect(
			encodeBrowserKey(key("ArrowLeft", { ctrlKey: true, shiftKey: true })),
		).toBe("\x1b[1;6D");
		expect(encodeBrowserKey(key("Tab", { shiftKey: true }))).toBe("\x1b[Z");
	});

	test("uses Linux Alt prefixes without breaking macOS Option composition", () => {
		expect(encodeBrowserKey(key("x", { altKey: true }), "other")).toBe("\x1bx");
		expect(encodeBrowserKey(key("å", { altKey: true }), "mac")).toBeNull();
		expect(classifyBrowserPlatform("MacIntel")).toBe("mac");
		expect(classifyBrowserPlatform("Linux x86_64")).toBe("other");
	});

	test("leaves printable, composition, copy, and paste events native", () => {
		expect(encodeBrowserKey(key("x"))).toBeNull();
		expect(encodeBrowserKey(key("x", { isComposing: true }))).toBeNull();
		expect(encodeBrowserKey(key("c", { metaKey: true }))).toBeNull();
		expect(
			encodeBrowserKey(key("c", { ctrlKey: true, shiftKey: true })),
		).toBeNull();
		expect(encodeBrowserKey(key("v", { ctrlKey: true }))).toBeNull();
		expect(encodeBrowserKey(key("v", { metaKey: true }))).toBeNull();
		expect(encodeBrowserKey(key("Insert", { shiftKey: true }))).toBeNull();
		expect(encodeBrowserKey(key("Insert", { ctrlKey: true }))).toBeNull();
	});
});

describe("TerminalProtocolState", () => {
	test("tracks OpenTUI mouse modes including all-motion mode", () => {
		const state = new TerminalProtocolState();
		state.feed(
			new TextEncoder().encode(
				"\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h\x1b[?2004h",
			),
		);
		expect(state.mouseTracking).toBe(1003);
		expect(state.mouseSgr).toBe(true);
		expect(state.bracketedPaste).toBe(true);
		state.feed(new TextEncoder().encode("\x1b[?1003l\x1b[?2004l"));
		expect(state.mouseTracking).toBe(1002);
		expect(state.bracketedPaste).toBe(false);
	});

	test("parses mode sequences split across websocket frames", () => {
		const state = new TerminalProtocolState();
		state.feed(new TextEncoder().encode("\x1b[?10"));
		state.feed(new TextEncoder().encode("03;1006h"));
		expect(state.mouseTracking).toBe(1003);
		expect(state.mouseSgr).toBe(true);
		state.feed(new TextEncoder().encode("\x1b[?1000;1002;1003;1006l"));
		expect(state.mouseTracking).toBe(0);
		expect(state.mouseSgr).toBe(false);
	});
});
