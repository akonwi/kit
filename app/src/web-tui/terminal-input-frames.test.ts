import { describe, expect, test } from "bun:test";
import {
	MAX_TERMINAL_INPUT_FRAME_BYTES,
	terminalInputFrames,
} from "./terminal-input-frames";

describe("terminalInputFrames", () => {
	test("keeps ordinary input in one frame", () => {
		const frames = terminalInputFrames("hello");
		expect(frames).toHaveLength(1);
		expect(new TextDecoder().decode(frames[0])).toBe("hello");
	});

	test("chunks large Unicode paste without losing bytes", () => {
		const value = `\x1b[200~${"λ🙂\n".repeat(20_000)}\x1b[201~`;
		const expected = new TextEncoder().encode(value);
		const frames = terminalInputFrames(value);
		expect(frames.length).toBeGreaterThan(1);
		expect(
			frames.every(
				(frame) => frame.byteLength <= MAX_TERMINAL_INPUT_FRAME_BYTES,
			),
		).toBe(true);
		expect(Buffer.concat(frames.map((frame) => Buffer.from(frame)))).toEqual(
			Buffer.from(expected),
		);
	});

	test("does not emit an empty WebSocket frame", () => {
		expect(terminalInputFrames("")).toEqual([]);
	});
});
