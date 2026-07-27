export const DEFAULT_WORKSPACE_PANE_RATIO = 0.4;

export type WorkspaceFocusedSurface = "transcript" | "composer" | "secondary";

export type WorkspaceNarrowTab = "transcript" | "secondary";

export type WorkspaceSecondaryState<TPane extends { kind: string }> =
	| { status: "empty" }
	| {
			status: "open" | "minimized";
			pane: TPane;
			returnPane?: TPane;
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
	setActiveSecondary(pane: TPane): void;
	openSecondary(
		pane: TPane,
		options?: { focus?: WorkspaceFocusedSurface },
	): void;
	pushSecondary(
		pane: TPane,
		options?: { focus?: WorkspaceFocusedSurface },
	): void;
	replaceSecondary(
		pane: TPane,
		options?: { focus?: WorkspaceFocusedSurface },
	): void;
	popSecondary(options?: { focus?: WorkspaceFocusedSurface }): boolean;
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
	initial: Partial<Omit<WorkspaceState<TPane>, "secondary">> & {
		secondary?: WorkspaceSecondaryState<TPane>;
	} = {},
): WorkspaceStateController<TPane> {
	const initialSecondary = initial.secondary ?? { status: "empty" };
	const initialFocus = initial.focusedSurface ?? "composer";
	const initialNarrowTab = initial.narrowTab ?? "transcript";
	const canSelectSecondaryTab = initialSecondary.status === "open";
	let state: WorkspaceState<TPane> = {
		secondary: initialSecondary,
		preferredPaneRatio: normalizePreferredRatio(
			initial.preferredPaneRatio ?? DEFAULT_WORKSPACE_PANE_RATIO,
		),
		focusedSurface:
			initialFocus === "secondary" && initialSecondary.status !== "open"
				? "composer"
				: initialFocus,
		narrowTab:
			initialNarrowTab === "secondary" && !canSelectSecondaryTab
				? "transcript"
				: initialNarrowTab,
	};
	const listeners = new Set<(state: WorkspaceState<TPane>) => void>();

	function update(next: WorkspaceState<TPane>): void {
		if (next === state) return;
		state = next;
		for (const listener of listeners) listener(state);
	}

	return {
		getState: () => state,
		subscribe(listener) {
			listeners.add(listener);
			return () => listeners.delete(listener);
		},
		setActiveSecondary(pane) {
			const status =
				state.secondary.status === "empty"
					? "minimized"
					: state.secondary.status;
			update({
				...state,
				secondary: { status, pane },
				focusedSurface:
					state.focusedSurface === "secondary"
						? "composer"
						: state.focusedSurface,
				narrowTab: "transcript",
			});
		},
		openSecondary(pane, options) {
			update({
				...state,
				secondary: { status: "open", pane },
				focusedSurface: options?.focus ?? state.focusedSurface,
				narrowTab: state.narrowTab,
			});
		},
		pushSecondary(pane, options) {
			const returnPane =
				state.secondary.status === "open" ? state.secondary.pane : undefined;
			update({
				...state,
				secondary: returnPane
					? { status: "open", pane, returnPane }
					: { status: "open", pane },
				focusedSurface: options?.focus ?? state.focusedSurface,
				narrowTab: state.narrowTab,
			});
		},
		replaceSecondary(pane, options) {
			const returnPane =
				state.secondary.status === "open"
					? state.secondary.returnPane
					: undefined;
			update({
				...state,
				secondary: returnPane
					? { status: "open", pane, returnPane }
					: { status: "open", pane },
				focusedSurface: options?.focus ?? state.focusedSurface,
				narrowTab: state.narrowTab,
			});
		},
		popSecondary(options) {
			if (state.secondary.status !== "open" || !state.secondary.returnPane) {
				return false;
			}
			update({
				...state,
				secondary: {
					status: "open",
					pane: state.secondary.returnPane,
				},
				focusedSurface: options?.focus ?? state.focusedSurface,
				narrowTab: "secondary",
			});
			return true;
		},
		restoreSecondary(options) {
			if (state.secondary.status !== "minimized") return false;
			update({
				...state,
				secondary: { ...state.secondary, status: "open" },
				focusedSurface: options?.focus ?? state.focusedSurface,
			});
			return true;
		},
		minimizeSecondary() {
			if (state.secondary.status !== "open") return;
			update({
				...state,
				secondary: {
					status: "minimized",
					pane: state.secondary.pane,
					...(state.secondary.returnPane
						? { returnPane: state.secondary.returnPane }
						: {}),
				},
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
