import { describe, expect, test } from "bun:test";
import {
	activateExistingActivityTab,
	shouldRestoreComposerFocus,
} from "./AppShell";

describe("activateExistingActivityTab", () => {
	test("updates and activates an existing inactive Activity tab", () => {
		const updates: unknown[] = [];
		const activations: string[] = [];
		const source = { kind: "single-item", itemId: "assistant:1" } as const;

		activateExistingActivityTab({
			tabId: "workspace-tab:activity",
			source,
			update: (pane) => updates.push(pane),
			activate: (tabId) => activations.push(tabId),
		});

		expect(updates).toEqual([{ kind: "activity", source }]);
		expect(activations).toEqual(["workspace-tab:activity"]);
	});
});

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
