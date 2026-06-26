import { describe, expect, test } from "bun:test";
import { shouldHandleScratchpadFocusNext } from "./AppShell";

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
