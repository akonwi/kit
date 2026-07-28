import { describe, expect, test } from "bun:test";
import {
	supportsTerminalProgress,
	terminalProgressSequence,
} from "./terminal-progress";

describe("terminal progress", () => {
	test("detects Ghostty through TERM_PROGRAM or TERM", () => {
		expect(supportsTerminalProgress({ TERM_PROGRAM: "ghostty" })).toBe(true);
		expect(supportsTerminalProgress({ TERM: "xterm-ghostty" })).toBe(true);
		expect(supportsTerminalProgress({ TERM: "xterm-256color" })).toBe(false);
		expect(
			supportsTerminalProgress({ TERM_PROGRAM: "ghostty", TMUX: "/tmp/tmux" }),
		).toBe(false);
		expect(
			supportsTerminalProgress({ TERM: "xterm-ghostty", STY: "screen" }),
		).toBe(false);
	});

	test("formats Ghostty OSC 9;4 progress reports", () => {
		expect(terminalProgressSequence("indeterminate")).toBe(
			"\u001B]9;4;3\u001B\\",
		);
		expect(terminalProgressSequence("remove")).toBe("\u001B]9;4;0\u001B\\");
	});
});
