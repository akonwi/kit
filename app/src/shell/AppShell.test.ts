import { describe, expect, test } from "bun:test";
import { shouldRestoreComposerFocus } from "./AppShell";

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
