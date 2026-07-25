import { describe, expect, test } from "bun:test";
import {
	shouldHandleScratchpadFocusNext,
	shouldRestoreComposerFocus,
} from "./AppShell";

describe("shouldRestoreComposerFocus", () => {
	const available = {
		overlayOpen: false,
		chromeOverflowOpen: false,
		pickerVisible: false,
		commandPaletteVisible: false,
		focusedSurface: "composer" as const,
	};

	test("restores focus when the composer still owns the primary surface", () => {
		expect(shouldRestoreComposerFocus(available)).toBeTrue();
	});

	test("does not steal focus from transient or modal surfaces", () => {
		expect(
			shouldRestoreComposerFocus({ ...available, overlayOpen: true }),
		).toBeFalse();
		expect(
			shouldRestoreComposerFocus({ ...available, pickerVisible: true }),
		).toBeFalse();
		expect(
			shouldRestoreComposerFocus({ ...available, commandPaletteVisible: true }),
		).toBeFalse();
		expect(
			shouldRestoreComposerFocus({ ...available, chromeOverflowOpen: true }),
		).toBeFalse();
	});

	test("does not restore focus after ownership moves to the secondary pane", () => {
		expect(
			shouldRestoreComposerFocus({
				...available,
				focusedSurface: "secondary",
			}),
		).toBeFalse();
	});
});

describe("shouldHandleScratchpadFocusNext", () => {
	test("handles Tab only when the scratchpad is open and no picker has priority", () => {
		expect(
			shouldHandleScratchpadFocusNext({
				scratchpadOpen: true,
				overlayOpen: false,
				pickerVisible: false,
				commandPaletteVisible: false,
			}),
		).toBe(true);
	});

	test("yields Tab to the command palette picker", () => {
		expect(
			shouldHandleScratchpadFocusNext({
				scratchpadOpen: true,
				overlayOpen: false,
				pickerVisible: false,
				commandPaletteVisible: true,
			}),
		).toBe(false);
	});

	test("yields Tab to overlays and inline pickers", () => {
		expect(
			shouldHandleScratchpadFocusNext({
				scratchpadOpen: true,
				overlayOpen: true,
				pickerVisible: false,
				commandPaletteVisible: false,
			}),
		).toBe(false);
		expect(
			shouldHandleScratchpadFocusNext({
				scratchpadOpen: true,
				overlayOpen: false,
				pickerVisible: true,
				commandPaletteVisible: false,
			}),
		).toBe(false);
	});
});
