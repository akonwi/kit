/** @jsxImportSource solid-js */
import {
	type Accessor,
	createContext,
	createEffect,
	createMemo,
	createSignal,
	type JSX,
	onCleanup,
	useContext,
} from "solid-js";
import { findTurnWorkItems } from "../shell/transcript/turns";
import {
	createWorkspaceStateController,
	type WorkspaceState,
} from "../shell/workspace-state";
import { useCodeReview } from "./CodeReviewProvider";
import { useScratchpad } from "./ScratchpadProvider";
import { useWebClient } from "./WebClientContext";
import {
	type ActivitySource,
	paneClosable,
	paneIdentity,
	type WebWorkspacePane,
	type WebWorkspaceTab,
} from "./workspace-panes";

export type { ActivitySource } from "./workspace-panes";

function activitySourceKey(source: ActivitySource): string {
	return source.kind === "single-item"
		? `single:${source.itemId}`
		: `turn:${source.turnId}:${source.anchorItemId}`;
}

function activitySourcesMatch(a: ActivitySource, b: ActivitySource): boolean {
	if (activitySourceKey(a) === activitySourceKey(b)) return true;
	if (a.turnId !== b.turnId) return false;
	return a.kind === "single-item"
		? b.kind === "turn-intermediate" && a.itemId === b.anchorItemId
		: b.kind === "single-item" && a.anchorItemId === b.itemId;
}

type WorkspaceContextValue = {
	state: Accessor<WorkspaceState<WebWorkspacePane>>;
	tabs: Accessor<readonly WebWorkspaceTab[]>;
	activeTab: Accessor<WebWorkspaceTab | null>;
	openActivity(source: ActivitySource): void;
	isActivityOpen(source: ActivitySource): boolean;
	openCodeReview(): void;
	toggleScratchpad(): void;
	selectTranscript(): void;
	selectTab(tabId: string): boolean;
	closeTab(tabId: string): boolean;
	clear(): void;
};

export const WorkspaceContext = createContext<WorkspaceContextValue>();

export function WorkspaceProvider(props: {
	children: JSX.Element;
}): JSX.Element {
	const { snapshot, transcriptItems } = useWebClient();
	const scratchpad = useScratchpad();
	const codeReview = useCodeReview();
	const controller = createWorkspaceStateController<WebWorkspacePane>({
		focusedSurface: "transcript",
		identityOf: paneIdentity,
	});
	const [state, setState] = createSignal(controller.getState());
	onCleanup(controller.subscribe(setState));

	const tabs = createMemo<readonly WebWorkspaceTab[]>(() => {
		const secondary = state().secondary;
		return secondary.status === "empty" ? [] : secondary.tabs;
	});
	const activeTab = createMemo<WebWorkspaceTab | null>(() => {
		const secondary = state().secondary;
		if (secondary.status === "empty") return null;
		return (
			secondary.tabs.find((tab) => tab.id === secondary.activeTabId) ?? null
		);
	});

	function openPane(pane: WebWorkspacePane): void {
		controller.openSecondary(pane, { focus: "secondary" });
	}

	function openActivity(source: ActivitySource): void {
		if (!controller.updateSecondary({ kind: "activity", source })) {
			openPane({ kind: "activity", source });
			return;
		}
		const activity = tabs().find((tab) => tab.pane.kind === "activity");
		if (activity)
			controller.selectSecondary(activity.id, { focus: "secondary" });
	}

	function isActivityOpen(source: ActivitySource): boolean {
		const activity = tabs().find((tab) => tab.pane.kind === "activity");
		if (activity?.pane.kind !== "activity") return false;
		const current = activity.pane.source;
		if (activitySourcesMatch(current, source)) return true;
		if (
			current.kind !== "turn-intermediate" ||
			source.kind !== "turn-intermediate" ||
			current.turnId !== source.turnId
		) {
			return false;
		}
		return findTurnWorkItems(
			transcriptItems(),
			current.turnId,
			snapshot().protocol.activeTurnId,
			current.anchorItemId,
		).some((item) => item.id === source.anchorItemId);
	}

	function openCodeReview(): void {
		codeReview.openReview();
		openPane({ kind: "review" });
	}

	function toggleScratchpad(): void {
		const scratchpadTab = tabs().find((tab) => tab.pane.kind === "scratchpad");
		if (
			scratchpadTab &&
			activeTab()?.id === scratchpadTab.id &&
			state().focusedSurface === "secondary"
		) {
			controller.setFocusedSurface("transcript");
			controller.setNarrowTab("transcript");
			return;
		}
		if (!scratchpad.open()) scratchpad.toggle();
		openPane({ kind: "scratchpad" });
	}

	function selectTranscript(): void {
		controller.setFocusedSurface("transcript");
		controller.setNarrowTab("transcript");
	}

	function selectTab(tabId: string): boolean {
		return controller.selectSecondary(tabId, { focus: "secondary" });
	}

	function closeTab(tabId: string): boolean {
		const tab = tabs().find((candidate) => candidate.id === tabId);
		if (!tab || !paneClosable(tab.pane)) return false;
		return controller.closeSecondary(tabId);
	}

	function clear(): void {
		controller.clearSecondary();
	}

	let observedSessionId: unknown;
	createEffect(() => {
		const sessionId = snapshot().protocol.serverState.sessionId;
		if (observedSessionId !== undefined && sessionId !== observedSessionId) {
			const restoreTranscriptFocus =
				document.activeElement instanceof Element &&
				document.activeElement.closest(
					".workspace-secondary, .workspace-drawer-tabs, .workspace-tabs, .workspace-divider",
				) !== null;
			clear();
			if (restoreTranscriptFocus) {
				queueMicrotask(() =>
					document.querySelector<HTMLElement>("#transcript")?.focus(),
				);
			}
		}
		observedSessionId = sessionId;
	});

	return (
		<WorkspaceContext.Provider
			value={{
				state,
				tabs,
				activeTab,
				openActivity,
				isActivityOpen,
				openCodeReview,
				toggleScratchpad,
				selectTranscript,
				selectTab,
				closeTab,
				clear,
			}}
		>
			{props.children}
		</WorkspaceContext.Provider>
	);
}

export function useWorkspace(): WorkspaceContextValue {
	const value = useContext(WorkspaceContext);
	if (!value) throw new Error("WorkspaceProvider is missing");
	return value;
}
