import { describe, expect, test } from "bun:test";
import {
	resolveNarrowWorkspaceTabs,
	resolveWideWorkspaceTabs,
	revealWideWorkspaceTab,
} from "./workspace-tabs-layout";

const tabs = [
	{ id: "activity", label: "Activity" },
	{ id: "review", label: "Code review" },
	{ id: "scratchpad", label: "Scratchpad" },
	{ id: "releases", label: "Release notes" },
];

describe("workspace tab layout", () => {
	test("packs wide tabs and reports hidden tabs on both edges", () => {
		const first = resolveWideWorkspaceTabs({ tabs, width: 24, offset: 0 });
		expect(first.hiddenBefore).toBe(0);
		expect(first.hiddenAfter).toBeGreaterThan(0);

		const later = resolveWideWorkspaceTabs({ tabs, width: 24, offset: 2 });
		expect(later.hiddenBefore).toBe(2);
		expect(later.visible[0]?.id).toBe("scratchpad");
	});

	test("reveals an active wide tab without reordering", () => {
		const offset = revealWideWorkspaceTab({
			tabs,
			width: 24,
			offset: 0,
			activeTabId: "releases",
		});
		expect(offset).toBeGreaterThan(0);
		expect(
			resolveWideWorkspaceTabs({ tabs, width: 24, offset }).visible.some(
				(tab) => tab.id === "releases",
			),
		).toBeTrue();
	});

	test("pins Transcript and collapses narrow overflow", () => {
		const layout = resolveNarrowWorkspaceTabs({ tabs, width: 30 });
		expect(layout.visible[0]?.id).toBe("transcript");
		expect(layout.overflow.length).toBeGreaterThan(0);
		expect(layout.visible.map((tab) => tab.id)).toEqual(
			["transcript", ...tabs.map((tab) => tab.id)].slice(
				0,
				layout.visible.length,
			),
		);
	});

	test("reserves narrow overflow space by truncating Transcript", () => {
		const layout = resolveNarrowWorkspaceTabs({ tabs, width: 16 });
		expect(layout.visible).toHaveLength(1);
		expect(layout.visible[0]?.id).toBe("transcript");
		expect(layout.visible[0]?.width).toBeLessThanOrEqual(13);
		expect(layout.overflow).toHaveLength(tabs.length);
	});

	test("uses terminal cell widths for labels", () => {
		const layout = resolveWideWorkspaceTabs({
			tabs: [{ id: "wide", label: "図表" }],
			width: 12,
			offset: 0,
		});
		expect(layout.visible[0]?.width).toBe(8);
	});
});
