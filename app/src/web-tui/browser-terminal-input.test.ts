import { describe, expect, test } from "bun:test";
import {
	type BrowserKeyLike,
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
	test("normalizes Escape and control characters", () => {
		expect(encodeBrowserKey(key("Escape"))).toBe("\x1b");
		expect(encodeBrowserKey(key("c", { ctrlKey: true }))).toBe("\x03");
		expect(encodeBrowserKey(key("[", { ctrlKey: true }))).toBe("\x1b");
	});

	test("encodes navigation keys and their modifiers", () => {
		expect(encodeBrowserKey(key("ArrowUp"))).toBe("\x1b[A");
		expect(
			encodeBrowserKey(key("ArrowLeft", { ctrlKey: true, shiftKey: true })),
		).toBe("\x1b[1;6D");
		expect(encodeBrowserKey(key("Tab", { shiftKey: true }))).toBe("\x1b[Z");
	});

	test("leaves printable, composition, copy, and paste events native", () => {
		expect(encodeBrowserKey(key("x"))).toBeNull();
		expect(encodeBrowserKey(key("x", { isComposing: true }))).toBeNull();
		expect(encodeBrowserKey(key("c", { metaKey: true }))).toBeNull();
		expect(
			encodeBrowserKey(key("c", { ctrlKey: true, shiftKey: true })),
		).toBeNull();
		expect(encodeBrowserKey(key("v", { ctrlKey: true }))).toBeNull();
	});
});

describe("TerminalProtocolState", () => {
	test("tracks OpenTUI mouse modes including all-motion mode", () => {
		const state = new TerminalProtocolState();
		state.feed(
			new TextEncoder().encode("\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h"),
		);
		expect(state.mouseTracking).toBe(1003);
		expect(state.mouseSgr).toBe(true);
		state.feed(new TextEncoder().encode("\x1b[?1003l"));
		expect(state.mouseTracking).toBe(1002);
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
