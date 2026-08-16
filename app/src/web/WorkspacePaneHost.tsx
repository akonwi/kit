/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	type JSX,
	onCleanup,
	onMount,
	Show,
} from "solid-js";
import { CHEVRON_LEFT } from "../shell/glyphs";
import { CodeReviewPanel } from "./CodeReviewPanel";
import { ScratchpadPanel } from "./ScratchpadPanel";
import { TurnActivityPanel } from "./TurnActivityPanel";
import {
	DesktopWorkspaceTabs,
	focusWorkspaceTab,
	NarrowWorkspaceTabs,
} from "./WorkspaceTabNavigation";
import { useWorkspace } from "./workspace-context";
import { paneLabel } from "./workspace-panes";

const DEFAULT_SECONDARY_RATIO = 0.4;
const MIN_PRIMARY_WIDTH = 560;
const MIN_SECONDARY_WIDTH = 320;
const COLLAPSED_RAIL_WIDTH = 38;
const PANE_RATIO_STORAGE_KEY = "kit.web.workspace.secondaryRatio";
const RESIZE_STEP = 0.05;
const DIVIDER_WIDTH = 5;

function clampRatio(ratio: number): number {
	if (!Number.isFinite(ratio)) return DEFAULT_SECONDARY_RATIO;
	return Math.max(0.1, Math.min(0.9, ratio));
}

export function WorkspacePaneHost(props: {
	primary: JSX.Element;
	dock: JSX.Element;
}): JSX.Element {
	const workspace = useWorkspace();
	let host: HTMLElement | undefined;
	let activePointerId: number | null = null;
	let resizeStartRatio = DEFAULT_SECONDARY_RATIO;
	const [hostWidth, setHostWidth] = createSignal(0);
	const [drawerCollapsed, setDrawerCollapsed] = createSignal(true);
	const [secondaryRatio, setSecondaryRatio] = createSignal(
		DEFAULT_SECONDARY_RATIO,
	);
	const [dragging, setDragging] = createSignal(false);
	const tabs = workspace.tabs;
	const activeTab = workspace.activeTab;
	const selectedId = createMemo(() =>
		workspace.state().focusedSurface === "secondary"
			? (activeTab()?.id ?? "transcript")
			: "transcript",
	);
	const canSplit = createMemo(
		() =>
			hostWidth() >= MIN_PRIMARY_WIDTH + MIN_SECONDARY_WIDTH + DIVIDER_WIDTH,
	);
	const expanded = createMemo(
		() => canSplit() && tabs().length > 0 && !drawerCollapsed(),
	);
	const narrow = createMemo(() => !canSplit() && tabs().length > 0);
	const secondaryWidth = createMemo(() => {
		const width = hostWidth();
		return Math.max(
			MIN_SECONDARY_WIDTH,
			Math.min(
				width - MIN_PRIMARY_WIDTH - DIVIDER_WIDTH,
				width * secondaryRatio(),
			),
		);
	});
	const effectiveRatio = createMemo(() =>
		hostWidth() > 0 ? secondaryWidth() / hostWidth() : secondaryRatio(),
	);
	const minRatio = createMemo(() =>
		hostWidth() > 0 ? MIN_SECONDARY_WIDTH / hostWidth() : 0,
	);
	const maxRatio = createMemo(() =>
		hostWidth() > 0
			? (hostWidth() - MIN_PRIMARY_WIDTH - DIVIDER_WIDTH) / hostWidth()
			: 1,
	);

	function setRatio(ratio: number, persist = false): void {
		const constrained = Math.max(
			minRatio(),
			Math.min(maxRatio(), clampRatio(ratio)),
		);
		setSecondaryRatio(constrained);
		if (persist)
			localStorage.setItem(PANE_RATIO_STORAGE_KEY, String(constrained));
	}

	function ratioAt(clientX: number): number {
		const bounds = host?.getBoundingClientRect();
		if (!bounds || bounds.width <= 0) return secondaryRatio();
		return (bounds.right - clientX - DIVIDER_WIDTH / 2) / bounds.width;
	}

	function collapseDrawer(): void {
		setDrawerCollapsed(true);
		workspace.selectTranscript();
		queueMicrotask(() =>
			host?.querySelector<HTMLElement>(".workspace-collapsed-rail")?.focus(),
		);
	}

	function finishPointerResize(event: PointerEvent, persist: boolean): void {
		if (activePointerId !== event.pointerId) return;
		const target = event.currentTarget;
		const rawRatio = ratioAt(event.clientX);
		activePointerId = null;
		setDragging(false);
		if (persist && rawRatio * hostWidth() <= MIN_SECONDARY_WIDTH) {
			setSecondaryRatio(resizeStartRatio);
			collapseDrawer();
		} else if (persist) {
			setRatio(rawRatio, true);
		} else {
			setSecondaryRatio(resizeStartRatio);
		}
		if (
			target instanceof Element &&
			target.hasPointerCapture(event.pointerId)
		) {
			target.releasePointerCapture(event.pointerId);
		}
	}

	function selectTab(tabId: string): void {
		workspace.selectTab(tabId);
		if (canSplit()) setDrawerCollapsed(false);
	}

	function closeTab(tabId: string, restoreFocus = true): void {
		if (!workspace.closeTab(tabId) || !restoreFocus) return;
		const next = workspace.activeTab();
		if (next) focusWorkspaceTab(host, next.id);
		else {
			workspace.selectTranscript();
			queueMicrotask(() =>
				host?.querySelector<HTMLElement>("#transcript")?.focus(),
			);
		}
	}

	function closeUnavailableTab(tabId: string): void {
		const activeElement = document.activeElement;
		const focusedPane =
			activeElement instanceof Element
				? activeElement.closest(".workspace-pane-surface")
				: null;
		const focusedTab =
			activeElement instanceof Element
				? (activeElement.closest<HTMLElement>("[role=tab][data-tab-id]") ??
					activeElement
						.closest(".workspace-tab-item")
						?.querySelector<HTMLElement>("[role=tab][data-tab-id]"))
				: null;
		closeTab(
			tabId,
			focusedPane?.id === `workspace-pane-${tabId}` ||
				focusedTab?.dataset.tabId === tabId,
		);
	}

	let previousActiveId: string | undefined;
	let previousCanSplit = false;
	createEffect(() => {
		const splitAvailable = canSplit();
		const current =
			workspace.state().focusedSurface === "secondary"
				? activeTab()?.id
				: undefined;
		if (
			current &&
			splitAvailable &&
			(current !== previousActiveId || !previousCanSplit)
		) {
			setDrawerCollapsed(false);
		}
		if (tabs().length === 0) setDrawerCollapsed(true);
		previousActiveId = current;
		previousCanSplit = splitAvailable;
	});

	createEffect(() => {
		if (expanded() || !dragging()) return;
		activePointerId = null;
		setSecondaryRatio(resizeStartRatio);
		setDragging(false);
	});

	let previousTabCount = tabs().length;
	createEffect(() => {
		const tabCount = tabs().length;
		if (
			previousTabCount > 0 &&
			tabCount === 0 &&
			document.activeElement instanceof Element &&
			document.activeElement.closest(
				".workspace-secondary, .workspace-drawer-tabs, .workspace-tabs, .workspace-divider",
			)
		) {
			queueMicrotask(() =>
				host?.querySelector<HTMLElement>("#transcript")?.focus(),
			);
		}
		previousTabCount = tabCount;
	});

	let resizeObserver: ResizeObserver | undefined;
	onMount(() => {
		const storedRatio = Number(localStorage.getItem(PANE_RATIO_STORAGE_KEY));
		if (Number.isFinite(storedRatio) && storedRatio > 0) {
			setSecondaryRatio(clampRatio(storedRatio));
		}
		if (!host) return;
		resizeObserver = new ResizeObserver(([entry]) => {
			if (!entry) return;
			const wasSplit = canSplit();
			const willSplit =
				entry.contentRect.width >=
				MIN_PRIMARY_WIDTH + MIN_SECONDARY_WIDTH + DIVIDER_WIDTH;
			const activeElement = document.activeElement;
			const chromeOwnedFocus =
				activeElement instanceof Element &&
				activeElement.closest(
					wasSplit
						? ".workspace-drawer-tabs, .workspace-divider, .workspace-collapsed-rail"
						: ".workspace-tabs, .workspace-surface-sheet",
				) !== null;
			const targetId = selectedId();
			setHostWidth(entry.contentRect.width);
			if (wasSplit === willSplit || !chromeOwnedFocus) return;
			queueMicrotask(() => {
				if (targetId === "transcript") {
					const selector = willSplit
						? "#transcript"
						: '[role="tab"][data-tab-id="transcript"]';
					(
						host?.querySelector<HTMLElement>(selector) ??
						host?.querySelector<HTMLElement>("#transcript")
					)?.focus();
				} else {
					focusWorkspaceTab(host, targetId);
				}
			});
		});
		resizeObserver.observe(host);
	});
	onCleanup(() => resizeObserver?.disconnect());

	return (
		<section
			ref={host}
			class="workspace-pane-host"
			classList={{ "is-dragging": dragging() }}
			data-layout={expanded() ? "split" : narrow() ? "tabs" : "collapsed"}
			style={{
				"--workspace-divider-width": `${DIVIDER_WIDTH}px`,
				"--workspace-rail-width": `${COLLAPSED_RAIL_WIDTH}px`,
			}}
			aria-label="Session workspace"
			onFocusIn={(event) => {
				const target = event.target;
				if (!(target instanceof Element)) return;
				if (target.closest(".workspace-primary")) workspace.selectTranscript();
				else if (target.closest(".workspace-pane-surface")) {
					const active = activeTab();
					if (active) workspace.selectTab(active.id);
				}
			}}
			onKeyDown={(event) => {
				if (
					event.key !== "Escape" ||
					selectedId() === "transcript" ||
					(event.target instanceof Element && event.target.closest("dialog"))
				) {
					return;
				}
				event.preventDefault();
				workspace.selectTranscript();
				queueMicrotask(() =>
					host?.querySelector<HTMLElement>("#transcript")?.focus(),
				);
			}}
		>
			<Show when={narrow()}>
				<NarrowWorkspaceTabs
					tabs={tabs()}
					selectedId={selectedId()}
					onSelectTranscript={workspace.selectTranscript}
					onSelect={selectTab}
					onClose={closeTab}
				/>
			</Show>
			<div class="workspace-primary">
				<div
					id="workspace-transcript"
					class="workspace-primary-content"
					role={narrow() ? "tabpanel" : undefined}
					aria-labelledby={narrow() ? "workspace-tab-transcript" : undefined}
					aria-hidden={narrow() && selectedId() !== "transcript"}
					hidden={narrow() && selectedId() !== "transcript"}
				>
					{props.primary}
				</div>
				<div
					class="workspace-dock"
					hidden={narrow() && selectedId() !== "transcript"}
				>
					{props.dock}
				</div>
			</div>
			<Show when={expanded()}>
				<div
					class="workspace-divider"
					classList={{ "is-dragging": dragging() }}
					role="separator"
					aria-label="Resize workspace drawer"
					aria-orientation="vertical"
					aria-valuemin={Math.round(minRatio() * 100)}
					aria-valuemax={Math.round(maxRatio() * 100)}
					aria-valuenow={Math.round(effectiveRatio() * 100)}
					tabIndex={0}
					onDblClick={() => setRatio(DEFAULT_SECONDARY_RATIO, true)}
					onKeyDown={(event) => {
						if (event.key === "ArrowLeft") {
							event.preventDefault();
							setRatio(effectiveRatio() + RESIZE_STEP, true);
						} else if (event.key === "ArrowRight") {
							event.preventDefault();
							setRatio(effectiveRatio() - RESIZE_STEP, true);
						} else if (event.key === "Home") {
							event.preventDefault();
							setRatio(minRatio(), true);
						} else if (event.key === "End") {
							event.preventDefault();
							setRatio(maxRatio(), true);
						}
					}}
					onPointerDown={(event) => {
						if (event.button !== 0) return;
						event.preventDefault();
						event.currentTarget.setPointerCapture(event.pointerId);
						activePointerId = event.pointerId;
						resizeStartRatio = effectiveRatio();
						setDragging(true);
					}}
					onPointerMove={(event) => {
						if (!dragging() || activePointerId !== event.pointerId) return;
						setRatio(ratioAt(event.clientX));
					}}
					onPointerUp={(event) => finishPointerResize(event, true)}
					onPointerCancel={(event) => finishPointerResize(event, false)}
					onLostPointerCapture={(event) => {
						if (activePointerId !== event.pointerId) return;
						activePointerId = null;
						setSecondaryRatio(resizeStartRatio);
						setDragging(false);
					}}
				/>
			</Show>
			<Show when={tabs().length > 0}>
				<aside
					class="workspace-secondary"
					aria-label={narrow() ? undefined : "Workspace drawer"}
					aria-hidden={
						(canSplit() && !expanded()) ||
						(narrow() && selectedId() === "transcript")
					}
					hidden={
						(canSplit() && !expanded()) ||
						(narrow() && selectedId() === "transcript")
					}
					style={{ "--secondary-width": `${secondaryWidth()}px` }}
				>
					<Show when={expanded() && activeTab()}>
						<DesktopWorkspaceTabs
							tabs={tabs()}
							activeTabId={activeTab()?.id ?? ""}
							onSelect={selectTab}
							onClose={closeTab}
							onCollapse={collapseDrawer}
						/>
					</Show>
					<div class="workspace-pane-stack">
						<For each={tabs()}>
							{(tab) => (
								<section
									id={`workspace-pane-${tab.id}`}
									class="workspace-pane-surface"
									role="tabpanel"
									aria-label={paneLabel(tab.pane)}
									aria-hidden={activeTab()?.id !== tab.id}
									hidden={activeTab()?.id !== tab.id}
								>
									{tab.pane.kind === "activity" ? (
										<TurnActivityPanel
											source={tab.pane.source}
											active={selectedId() === tab.id}
											onUnavailable={() => closeUnavailableTab(tab.id)}
										/>
									) : tab.pane.kind === "review" ? (
										<CodeReviewPanel />
									) : (
										<ScratchpadPanel active={selectedId() === tab.id} />
									)}
								</section>
							)}
						</For>
					</div>
				</aside>
			</Show>
			<Show when={canSplit() && !expanded()}>
				<button
					type="button"
					class="workspace-collapsed-rail"
					data-variant="ghost"
					aria-label={
						tabs().length > 0
							? `Expand workspace drawer with ${tabs().length} ${tabs().length === 1 ? "tab" : "tabs"}`
							: "Open Scratchpad in workspace drawer"
					}
					onClick={() => {
						if (tabs().length === 0) workspace.toggleScratchpad();
						else {
							setDrawerCollapsed(false);
							const active = activeTab();
							if (active) workspace.selectTab(active.id);
						}
					}}
				>
					<span aria-hidden="true">{CHEVRON_LEFT}</span>
					<Show when={tabs().length > 0}>
						<span class="workspace-rail-count">{tabs().length}</span>
					</Show>
				</button>
			</Show>
		</section>
	);
}
