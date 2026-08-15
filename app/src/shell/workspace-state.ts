export const DEFAULT_WORKSPACE_PANE_RATIO = 0.4;

export type WorkspaceFocusedSurface = "transcript" | "composer" | "secondary";

export type WorkspaceNarrowTab = "transcript" | "secondary";

export type WorkspaceTab<TPane extends { kind: string }> = {
	id: string;
	identity: string;
	pane: TPane;
};

export type WorkspaceSecondaryState<TPane extends { kind: string }> =
	| { status: "empty" }
	| {
			status: "open" | "minimized";
			tabs: readonly WorkspaceTab<TPane>[];
			activeTabId: string;
	  };

export type WorkspaceState<TPane extends { kind: string }> = {
	secondary: WorkspaceSecondaryState<TPane>;
	preferredPaneRatio: number;
	focusedSurface: WorkspaceFocusedSurface;
	narrowTab: WorkspaceNarrowTab;
};

export type WorkspaceStateController<TPane extends { kind: string }> = {
	getState(): WorkspaceState<TPane>;
	subscribe(listener: (state: WorkspaceState<TPane>) => void): () => void;
	openSecondary(
		pane: TPane,
		options?: { focus?: WorkspaceFocusedSurface },
	): string;
	updateSecondary(pane: TPane): boolean;
	selectSecondary(
		tabId: string,
		options?: { focus?: WorkspaceFocusedSurface },
	): boolean;
	closeSecondary(tabId: string): boolean;
	cycleSurface(direction: -1 | 1): boolean;
	restoreSecondary(options?: { focus?: WorkspaceFocusedSurface }): boolean;
	minimizeSecondary(): void;
	clearSecondary(): void;
	setPreferredPaneRatio(ratio: number): void;
	setFocusedSurface(surface: WorkspaceFocusedSurface): void;
	setNarrowTab(tab: WorkspaceNarrowTab): void;
};

function normalizePreferredRatio(ratio: number): number {
	if (!Number.isFinite(ratio)) return DEFAULT_WORKSPACE_PANE_RATIO;
	return Math.max(0.1, Math.min(0.9, ratio));
}

export function createWorkspaceStateController<TPane extends { kind: string }>(
	initial: {
		preferredPaneRatio?: number;
		focusedSurface?: WorkspaceFocusedSurface;
		identityOf?: (pane: TPane) => string;
	} = {},
): WorkspaceStateController<TPane> {
	const initialFocus = initial.focusedSurface ?? "composer";
	const identityOf = initial.identityOf ?? ((pane: TPane) => pane.kind);
	let nextTabId = 1;
	let state: WorkspaceState<TPane> = {
		secondary: { status: "empty" },
		preferredPaneRatio: normalizePreferredRatio(
			initial.preferredPaneRatio ?? DEFAULT_WORKSPACE_PANE_RATIO,
		),
		focusedSurface: initialFocus === "secondary" ? "composer" : initialFocus,
		narrowTab: "transcript",
	};
	const listeners = new Set<(state: WorkspaceState<TPane>) => void>();

	function update(next: WorkspaceState<TPane>): void {
		if (next === state) return;
		state = next;
		for (const listener of listeners) listener(state);
	}

	function selectPrimary(): void {
		update({
			...state,
			focusedSurface: "composer",
			narrowTab: "transcript",
		});
	}

	return {
		getState: () => state,
		subscribe(listener) {
			listeners.add(listener);
			return () => listeners.delete(listener);
		},
		openSecondary(pane, options) {
			const identity = identityOf(pane);
			const existingTabs =
				state.secondary.status === "empty" ? [] : state.secondary.tabs;
			const existing = existingTabs.find((tab) => tab.identity === identity);
			const tab = existing ?? {
				id: `workspace-tab:${nextTabId++}`,
				identity,
				pane,
			};
			const tabs = existing ? existingTabs : [...existingTabs, tab];
			update({
				...state,
				secondary: { status: "open", tabs, activeTabId: tab.id },
				focusedSurface: options?.focus ?? state.focusedSurface,
				narrowTab:
					options?.focus === "secondary" ? "secondary" : state.narrowTab,
			});
			return tab.id;
		},
		updateSecondary(pane) {
			if (state.secondary.status === "empty") return false;
			const identity = identityOf(pane);
			const index = state.secondary.tabs.findIndex(
				(tab) => tab.identity === identity,
			);
			if (index < 0) return false;
			const current = state.secondary.tabs[index];
			if (!current || current.pane === pane) return false;
			const tabs = [...state.secondary.tabs];
			tabs[index] = { ...current, pane };
			update({ ...state, secondary: { ...state.secondary, tabs } });
			return true;
		},
		selectSecondary(tabId, options) {
			if (state.secondary.status === "empty") return false;
			if (!state.secondary.tabs.some((tab) => tab.id === tabId)) return false;
			const focusedSurface = options?.focus ?? state.focusedSurface;
			if (
				state.secondary.status === "open" &&
				state.secondary.activeTabId === tabId &&
				state.focusedSurface === focusedSurface &&
				state.narrowTab === "secondary"
			) {
				return true;
			}
			update({
				...state,
				secondary: { ...state.secondary, status: "open", activeTabId: tabId },
				focusedSurface,
				narrowTab: "secondary",
			});
			return true;
		},
		closeSecondary(tabId) {
			if (state.secondary.status === "empty") return false;
			const index = state.secondary.tabs.findIndex((tab) => tab.id === tabId);
			if (index < 0) return false;
			const tabs = state.secondary.tabs.filter((tab) => tab.id !== tabId);
			if (tabs.length === 0) {
				update({
					...state,
					secondary: { status: "empty" },
					focusedSurface:
						state.focusedSurface === "secondary"
							? "composer"
							: state.focusedSurface,
					narrowTab: "transcript",
				});
				return true;
			}
			const closingActive = state.secondary.activeTabId === tabId;
			const nextActive = closingActive
				? (tabs[Math.min(index, tabs.length - 1)]?.id ?? tabs[0]?.id)
				: state.secondary.activeTabId;
			if (!nextActive) return false;
			update({
				...state,
				secondary: { ...state.secondary, tabs, activeTabId: nextActive },
			});
			return true;
		},
		cycleSurface(direction) {
			const secondary = state.secondary;
			if (secondary.status === "empty") return false;
			const tabs = secondary.tabs;
			const currentIndex =
				state.focusedSurface === "secondary"
					? tabs.findIndex((tab) => tab.id === secondary.activeTabId) + 1
					: 0;
			const nextIndex =
				(currentIndex + direction + tabs.length + 1) % (tabs.length + 1);
			if (nextIndex === 0) {
				selectPrimary();
				return true;
			}
			const tab = tabs[nextIndex - 1];
			if (!tab) return false;
			update({
				...state,
				secondary: { ...secondary, status: "open", activeTabId: tab.id },
				focusedSurface: "secondary",
				narrowTab: "secondary",
			});
			return true;
		},
		restoreSecondary(options) {
			if (state.secondary.status !== "minimized") return false;
			const focusedSurface = options?.focus ?? state.focusedSurface;
			update({
				...state,
				secondary: { ...state.secondary, status: "open" },
				focusedSurface,
				narrowTab:
					focusedSurface === "secondary" ? "secondary" : state.narrowTab,
			});
			return true;
		},
		minimizeSecondary() {
			if (state.secondary.status !== "open") return;
			update({
				...state,
				secondary: { ...state.secondary, status: "minimized" },
				focusedSurface:
					state.focusedSurface === "secondary"
						? "composer"
						: state.focusedSurface,
				narrowTab: "transcript",
			});
		},
		clearSecondary() {
			if (state.secondary.status === "empty") return;
			update({
				...state,
				secondary: { status: "empty" },
				focusedSurface:
					state.focusedSurface === "secondary"
						? "composer"
						: state.focusedSurface,
				narrowTab: "transcript",
			});
		},
		setPreferredPaneRatio(ratio) {
			const preferredPaneRatio = normalizePreferredRatio(ratio);
			if (preferredPaneRatio === state.preferredPaneRatio) return;
			update({ ...state, preferredPaneRatio });
		},
		setFocusedSurface(focusedSurface) {
			if (focusedSurface === "secondary" && state.secondary.status !== "open") {
				return;
			}
			if (focusedSurface === state.focusedSurface) return;
			update({ ...state, focusedSurface });
		},
		setNarrowTab(narrowTab) {
			if (narrowTab === "secondary" && state.secondary.status !== "open") {
				return;
			}
			if (narrowTab === state.narrowTab) return;
			update({ ...state, narrowTab });
		},
	};
}
