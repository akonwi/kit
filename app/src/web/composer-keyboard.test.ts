import { describe, expect, test } from "bun:test";
import {
	hasComposerPayload,
	shouldSubmitComposerKey,
} from "./composer-keyboard";

describe("composer payload", () => {
	test("requires non-whitespace text or an attachment", () => {
		expect(hasComposerPayload("", 0)).toBe(false);
		expect(hasComposerPayload("   \n", 0)).toBe(false);
		expect(hasComposerPayload("message", 0)).toBe(true);
		expect(hasComposerPayload("", 1)).toBe(true);
	});
});

describe("mobile composer keyboard behavior", () => {
	test("submits an unmodified Enter press for fine pointers", () => {
		expect(
			shouldSubmitComposerKey(
				{ key: "Enter", shiftKey: false, isComposing: false },
				false,
			),
		).toBe(true);
	});

	test("keeps Enter available for multiline input on coarse pointers", () => {
		expect(
			shouldSubmitComposerKey(
				{ key: "Enter", shiftKey: false, isComposing: false },
				true,
			),
		).toBe(false);
	});

	test("does not submit modified or composing key presses", () => {
		expect(
			shouldSubmitComposerKey(
				{ key: "Enter", shiftKey: true, isComposing: false },
				false,
			),
		).toBe(false);
		expect(
			shouldSubmitComposerKey(
				{ key: "Enter", shiftKey: false, isComposing: true },
				false,
			),
		).toBe(false);
		expect(
			shouldSubmitComposerKey(
				{ key: "a", shiftKey: false, isComposing: false },
				false,
			),
		).toBe(false);
	});
});
