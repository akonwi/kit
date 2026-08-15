import { describe, expect, test } from "bun:test";
import {
	preferredPaneRatioFromDivider,
	resolveWorkspacePaneLayout,
} from "./workspace-layout";

describe("workspace pane layout", () => {
	test("uses the preferred ratio when both surfaces remain useful", () => {
		expect(
			resolveWorkspacePaneLayout({
				availableColumns: 200,
				preferredPaneRatio: 0.4,
				minPrimaryColumns: 70,
				minSecondaryColumns: 30,
			}),
		).toEqual({ primaryColumns: 120, secondaryColumns: 80 });
	});

	test("clamps presentation without changing the preferred ratio", () => {
		expect(
			resolveWorkspacePaneLayout({
				availableColumns: 120,
				preferredPaneRatio: 0.8,
				minPrimaryColumns: 70,
				minSecondaryColumns: 30,
			}),
		).toEqual({ primaryColumns: 70, secondaryColumns: 50 });
	});

	test("declines to split when minimum useful widths cannot fit", () => {
		expect(
			resolveWorkspacePaneLayout({
				availableColumns: 99,
				preferredPaneRatio: 0.4,
				minPrimaryColumns: 70,
				minSecondaryColumns: 30,
			}),
		).toBeNull();
	});

	test("derives the preferred secondary ratio from a divider position", () => {
		expect(
			preferredPaneRatioFromDivider({
				availableColumns: 200,
				containerLeft: 10,
				dividerX: 130,
			}),
		).toBe(0.4);
	});
});
