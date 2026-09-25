import { describe, expect, test } from "bun:test";
import {
	createWorkspaceStateController,
	DEFAULT_WORKSPACE_PANE_RATIO,
	retainOpenedWorkspaceTabIds,
} from "./workspace-state";

type Pane =
	| { kind: "review" | "scratchpad"; id?: string }
	| { kind: "activity"; source: string };

function identityOf(pane: Pane): string {
	return pane.kind === "activity"
		? "activity"
		: `${pane.kind}:${pane.id ?? ""}`;
}

describe("workspace state", () => {
	test("starts with no tabs and transcript selected", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });

		expect(workspace.getState()).toEqual({
			secondary: { status: "empty" },
			preferredPaneRatio: DEFAULT_WORKSPACE_PANE_RATIO,
			focusedSurface: "composer",
			narrowTab: "transcript",
		});
	});

	test("seeds minimized default panes and restores them on reset", () => {
		const workspace = createWorkspaceStateController<Pane>({
			identityOf,
			initialPanes: [{ kind: "review" }, { kind: "scratchpad" }],
		});
		const initial = workspace.getState();

		expect(initial.secondary).toMatchObject({
			status: "minimized",
			tabs: [
				{ identity: "review:", pane: { kind: "review" } },
				{ identity: "scratchpad:", pane: { kind: "scratchpad" } },
			],
		});
		if (initial.secondary.status === "empty") {
			throw new Error("expected default tabs");
		}
		expect(initial.secondary.activeTabId).toBe(initial.secondary.tabs[0]?.id);
		expect(initial.focusedSurface).toBe("composer");
		expect(initial.narrowTab).toBe("transcript");

		workspace.openSecondary({ kind: "activity", source: "tool" });
		workspace.setPreferredPaneRatio(0.55);
		workspace.resetSecondary();
		const reset = workspace.getState();
		expect(
			reset.secondary.status === "empty"
				? []
				: reset.secondary.tabs.map((tab) => tab.pane.kind),
		).toEqual(["review", "scratchpad"]);
		expect(reset.secondary).toMatchObject({ status: "minimized" });
		expect(reset.preferredPaneRatio).toBe(0.55);
		expect(reset.focusedSurface).toBe("composer");
		expect(reset.narrowTab).toBe("transcript");
	});

	test("retains pane bodies only after their first expansion", () => {
		const workspace = createWorkspaceStateController<Pane>({
			identityOf,
			initialPanes: [{ kind: "review" }, { kind: "scratchpad" }],
		});
		let retained: ReadonlySet<string> = new Set();
		retained = retainOpenedWorkspaceTabIds(workspace.getState(), retained);
		expect(retained.size).toBe(0);

		expect(workspace.restoreSecondary()).toBeTrue();
		retained = retainOpenedWorkspaceTabIds(workspace.getState(), retained);
		expect(retained.size).toBe(1);
		const secondary = workspace.getState().secondary;
		if (secondary.status === "empty") throw new Error("expected default tabs");
		expect(retained.has(secondary.tabs[0]?.id ?? "")).toBeTrue();

		const scratchpadId = secondary.tabs[1]?.id;
		if (!scratchpadId) throw new Error("expected scratchpad tab");
		workspace.selectSecondary(scratchpadId);
		retained = retainOpenedWorkspaceTabIds(workspace.getState(), retained);
		expect(retained.size).toBe(2);

		workspace.resetSecondary();
		retained = retainOpenedWorkspaceTabIds(workspace.getState(), retained);
		expect(retained.size).toBe(0);
	});

	test("opens tabs in insertion order and deduplicates by identity", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		const reviewId = workspace.openSecondary(
			{ kind: "review", id: "draft" },
			{ focus: "secondary" },
		);
		const scratchpadId = workspace.openSecondary({ kind: "scratchpad" });
		const reopenedId = workspace.openSecondary({ kind: "review", id: "draft" });
		const secondary = workspace.getState().secondary;

		expect(secondary.status).toBe("open");
		if (secondary.status === "empty") throw new Error("expected tabs");
		expect(secondary.tabs.map((tab) => tab.id)).toEqual([
			reviewId,
			scratchpadId,
		]);
		expect(reopenedId).toBe(reviewId);
		expect(secondary.activeTabId).toBe(reviewId);
	});

	test("updates a singleton payload without selecting or expanding it", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		const activityId = workspace.openSecondary({
			kind: "activity",
			source: "first",
		});
		const reviewId = workspace.openSecondary({ kind: "review" });
		workspace.minimizeSecondary();

		expect(
			workspace.updateSecondary({ kind: "activity", source: "second" }),
		).toBeTrue();
		const secondary = workspace.getState().secondary;
		if (secondary.status === "empty") throw new Error("expected tabs");
		expect(secondary.status).toBe("minimized");
		expect(secondary.activeTabId).toBe(reviewId);
		expect(secondary.tabs.find((tab) => tab.id === activityId)?.pane).toEqual({
			kind: "activity",
			source: "second",
		});
	});

	test("retains tabs while collapsed and expands on selection", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		const reviewId = workspace.openSecondary({ kind: "review" });
		workspace.minimizeSecondary();
		expect(workspace.getState().secondary).toMatchObject({
			status: "minimized",
			activeTabId: reviewId,
		});
		expect(workspace.getState().focusedSurface).toBe("composer");

		expect(
			workspace.selectSecondary(reviewId, { focus: "secondary" }),
		).toBeTrue();
		expect(workspace.getState().secondary).toMatchObject({
			status: "open",
			activeTabId: reviewId,
		});
		expect(workspace.getState().narrowTab).toBe("secondary");
	});

	test("restores secondary focus and narrow selection atomically", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		workspace.openSecondary({ kind: "review" });
		workspace.minimizeSecondary();
		expect(workspace.restoreSecondary({ focus: "secondary" })).toBeTrue();
		expect(workspace.getState().focusedSurface).toBe("secondary");
		expect(workspace.getState().narrowTab).toBe("secondary");
	});

	test("closes the active tab to its right, then its left", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		const first = workspace.openSecondary({ kind: "review", id: "first" });
		const middle = workspace.openSecondary({ kind: "review", id: "middle" });
		const last = workspace.openSecondary({ kind: "review", id: "last" });
		workspace.selectSecondary(middle, { focus: "secondary" });

		expect(workspace.closeSecondary(middle)).toBeTrue();
		expect(workspace.getState().secondary).toMatchObject({ activeTabId: last });
		expect(workspace.closeSecondary(last)).toBeTrue();
		expect(workspace.getState().secondary).toMatchObject({
			activeTabId: first,
		});
		expect(workspace.closeSecondary(first)).toBeTrue();
		expect(workspace.getState().secondary).toEqual({ status: "empty" });
		expect(workspace.getState().focusedSurface).toBe("composer");
	});

	test("closing an inactive tab preserves the active tab", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		const first = workspace.openSecondary({ kind: "review", id: "first" });
		const second = workspace.openSecondary({ kind: "review", id: "second" });
		workspace.closeSecondary(first);
		expect(workspace.getState().secondary).toMatchObject({
			activeTabId: second,
		});
	});

	test("cycles forward and backward through transcript and tabs", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		const first = workspace.openSecondary({ kind: "review", id: "first" });
		const second = workspace.openSecondary({ kind: "review", id: "second" });
		workspace.setFocusedSurface("composer");

		workspace.cycleSurface(1);
		expect(workspace.getState().secondary).toMatchObject({
			activeTabId: first,
		});
		expect(workspace.getState().focusedSurface).toBe("secondary");
		workspace.cycleSurface(1);
		expect(workspace.getState().secondary).toMatchObject({
			activeTabId: second,
		});
		workspace.cycleSurface(1);
		expect(workspace.getState().focusedSurface).toBe("composer");
		workspace.cycleSurface(-1);
		expect(workspace.getState().secondary).toMatchObject({
			activeTabId: second,
		});
	});

	test("clears every tab without resetting the preferred ratio", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		workspace.openSecondary({ kind: "review" }, { focus: "secondary" });
		workspace.setPreferredPaneRatio(0.55);
		workspace.clearSecondary();

		expect(workspace.getState()).toEqual({
			secondary: { status: "empty" },
			preferredPaneRatio: 0.55,
			focusedSurface: "composer",
			narrowTab: "transcript",
		});
	});

	test("normalizes invalid preferred ratios", () => {
		const workspace = createWorkspaceStateController<Pane>({
			identityOf,
			preferredPaneRatio: Number.NaN,
		});
		expect(workspace.getState().preferredPaneRatio).toBe(
			DEFAULT_WORKSPACE_PANE_RATIO,
		);
		workspace.setPreferredPaneRatio(2);
		expect(workspace.getState().preferredPaneRatio).toBe(0.9);
		workspace.setPreferredPaneRatio(0);
		expect(workspace.getState().preferredPaneRatio).toBe(0.1);
	});

	test("ignores invalid tab operations", () => {
		const workspace = createWorkspaceStateController<Pane>({ identityOf });
		expect(workspace.selectSecondary("missing")).toBeFalse();
		expect(workspace.closeSecondary("missing")).toBeFalse();
		expect(
			workspace.updateSecondary({ kind: "activity", source: "x" }),
		).toBeFalse();
	});
});
