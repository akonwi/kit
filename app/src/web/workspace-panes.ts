import type { WorkspaceTab } from "../shell/workspace-state";

export type ActivitySource =
	| { kind: "single-item"; itemId: string; turnId: string }
	| {
			kind: "turn-intermediate";
			turnId: string;
			anchorItemId: string;
	  };

export type WebWorkspacePane =
	| { kind: "activity"; source: ActivitySource }
	| { kind: "review" }
	| { kind: "scratchpad" };

export type WebWorkspaceTab = WorkspaceTab<WebWorkspacePane>;

export function paneIdentity(pane: WebWorkspacePane): string {
	return pane.kind;
}

export function paneLabel(pane: WebWorkspacePane): string {
	if (pane.kind === "activity") return "Activity";
	if (pane.kind === "review") return "Code review";
	return "Scratchpad";
}

export function paneClosable(pane: WebWorkspacePane): boolean {
	return pane.kind === "activity";
}

export function directTabsForCount(
	tabs: readonly WebWorkspaceTab[],
	count: number,
	selectedId: string,
): readonly WebWorkspaceTab[] {
	const direct = tabs.slice(0, count);
	if (
		count === 0 ||
		selectedId === "transcript" ||
		direct.some((tab) => tab.id === selectedId)
	) {
		return direct;
	}
	const active = tabs.find((tab) => tab.id === selectedId);
	return active ? [...direct.slice(0, -1), active] : direct;
}
