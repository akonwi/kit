import { describe, expect, test } from "bun:test";
import { createWorkspaceStateController } from "../shell/workspace-state";
import {
	directTabsForCount,
	paneClosable,
	paneIdentity,
	paneLabel,
	type WebWorkspacePane,
} from "./workspace-panes";

describe("web workspace panes", () => {
	test("keeps surfaces in opening order and reuses singleton identities", () => {
		const workspace = createWorkspaceStateController<WebWorkspacePane>({
			identityOf: paneIdentity,
		});
		const review = workspace.openSecondary({ kind: "review" });
		const scratchpad = workspace.openSecondary({ kind: "scratchpad" });
		const reopenedReview = workspace.openSecondary({ kind: "review" });
		const secondary = workspace.getState().secondary;

		expect(reopenedReview).toBe(review);
		expect(scratchpad).not.toBe(review);
		expect(
			secondary.status === "empty" ? [] : secondary.tabs.map((tab) => tab.id),
		).toEqual([review, scratchpad]);
	});

	test("keeps the active overflow tab directly reachable without reordering", () => {
		const tabs = [
			{ id: "review", identity: "review", pane: { kind: "review" } as const },
			{
				id: "scratchpad",
				identity: "scratchpad",
				pane: { kind: "scratchpad" } as const,
			},
		];
		expect(
			directTabsForCount(tabs, 1, "scratchpad").map((tab) => tab.id),
		).toEqual(["scratchpad"]);
		expect(tabs.map((tab) => tab.id)).toEqual(["review", "scratchpad"]);
	});

	test("uses web labels and close policy", () => {
		expect(paneLabel({ kind: "review" })).toBe("Code review");
		expect(paneLabel({ kind: "scratchpad" })).toBe("Scratchpad");
		expect(
			paneLabel({
				kind: "activity",
				source: { kind: "single-item", itemId: "one", turnId: "turn" },
			}),
		).toBe("Activity");
		expect(paneClosable({ kind: "review" })).toBeFalse();
		expect(paneClosable({ kind: "scratchpad" })).toBeFalse();
		expect(
			paneClosable({
				kind: "activity",
				source: { kind: "single-item", itemId: "one", turnId: "turn" },
			}),
		).toBeTrue();
	});
});
