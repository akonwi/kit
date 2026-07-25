import { describe, expect, test } from "bun:test";
import {
	createWorkspaceStateController,
	DEFAULT_WORKSPACE_PANE_RATIO,
} from "./workspace-state";

type Pane = { kind: "review" | "scratchpad"; id?: string };

describe("workspace state", () => {
	test("starts with an empty secondary pane and transcript narrow tab", () => {
		const workspace = createWorkspaceStateController<Pane>();

		expect(workspace.getState()).toEqual({
			secondary: { status: "empty" },
			preferredPaneRatio: DEFAULT_WORKSPACE_PANE_RATIO,
			focusedSurface: "composer",
			narrowTab: "transcript",
		});
	});

	test("retains the active pane while minimized and restores it on open", () => {
		const workspace = createWorkspaceStateController<Pane>();
		const pane: Pane = { kind: "review", id: "draft" };

		workspace.openSecondary(pane, { focus: "secondary" });
		workspace.minimizeSecondary();
		expect(workspace.getState()).toEqual({
			secondary: { status: "minimized", pane },
			preferredPaneRatio: DEFAULT_WORKSPACE_PANE_RATIO,
			focusedSurface: "composer",
			narrowTab: "transcript",
		});

		workspace.openSecondary(pane, { focus: "secondary" });
		expect(workspace.getState().secondary).toEqual({
			status: "open",
			pane,
		});
		expect(workspace.getState().focusedSurface).toBe("secondary");
	});

	test("does not focus a secondary surface that is not open", () => {
		const workspace = createWorkspaceStateController<Pane>({
			focusedSurface: "secondary",
		});
		expect(workspace.getState().focusedSurface).toBe("composer");

		workspace.setFocusedSurface("secondary");
		expect(workspace.getState().focusedSurface).toBe("composer");
	});

	test("clears stale pane state and moves focus off the secondary surface", () => {
		const workspace = createWorkspaceStateController<Pane>();
		workspace.openSecondary({ kind: "scratchpad" }, { focus: "secondary" });

		workspace.clearSecondary();

		expect(workspace.getState().secondary).toEqual({ status: "empty" });
		expect(workspace.getState().focusedSurface).toBe("composer");
	});

	test("updates active pane content without forcing minimized state open", () => {
		const workspace = createWorkspaceStateController<Pane>();
		workspace.setActiveSecondary({ kind: "review", id: "first" });
		expect(workspace.getState().secondary).toEqual({
			status: "minimized",
			pane: { kind: "review", id: "first" },
		});

		workspace.setActiveSecondary({ kind: "review", id: "second" });
		expect(workspace.getState().secondary).toEqual({
			status: "minimized",
			pane: { kind: "review", id: "second" },
		});

		workspace.openSecondary({ kind: "scratchpad" }, { focus: "secondary" });
		workspace.setActiveSecondary({ kind: "review", id: "third" });
		expect(workspace.getState().secondary).toEqual({
			status: "open",
			pane: { kind: "review", id: "third" },
		});
		expect(workspace.getState().focusedSurface).toBe("composer");
	});

	test("models preferred ratio, workspace focus, and narrow tab explicitly", () => {
		const workspace = createWorkspaceStateController<Pane>();
		workspace.openSecondary({ kind: "review" });
		workspace.setPreferredPaneRatio(0.55);
		workspace.setFocusedSurface("transcript");
		workspace.setNarrowTab("review");

		expect(workspace.getState()).toMatchObject({
			preferredPaneRatio: 0.55,
			focusedSurface: "transcript",
			narrowTab: "review",
		});
	});

	test("keeps the review tab consistent with the active open pane", () => {
		const workspace = createWorkspaceStateController<Pane>();
		workspace.setNarrowTab("review");
		expect(workspace.getState().narrowTab).toBe("transcript");

		workspace.openSecondary({ kind: "review" });
		workspace.setNarrowTab("review");
		workspace.minimizeSecondary();
		expect(workspace.getState().narrowTab).toBe("transcript");

		workspace.openSecondary({ kind: "review" });
		workspace.setNarrowTab("review");
		workspace.openSecondary({ kind: "scratchpad" });
		expect(workspace.getState().narrowTab).toBe("transcript");
	});

	test("normalizes invalid preferred ratios without changing presentation state", () => {
		const workspace = createWorkspaceStateController<Pane>({
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

	test("notifies subscribers only when state changes", () => {
		const workspace = createWorkspaceStateController<Pane>();
		workspace.openSecondary({ kind: "review" });
		const states: ReturnType<typeof workspace.getState>[] = [];
		const unsubscribe = workspace.subscribe((state) => states.push(state));

		workspace.setNarrowTab("transcript");
		workspace.setNarrowTab("review");
		unsubscribe();
		workspace.setNarrowTab("transcript");

		expect(states).toHaveLength(1);
		expect(states[0]?.narrowTab).toBe("review");
	});
});
