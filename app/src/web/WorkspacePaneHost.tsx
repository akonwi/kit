/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	createSignal,
	type JSX,
	onCleanup,
	onMount,
	Show,
} from "solid-js";
import { findTurnWorkItems } from "../shell/transcript/turns";
import { CodeReviewPanel } from "./CodeReviewPanel";
import { useCodeReview } from "./CodeReviewProvider";
import { ScratchpadPanel } from "./ScratchpadPanel";
import { useScratchpad } from "./ScratchpadProvider";
import { TurnActivityPanel } from "./TurnActivityPanel";
import { useWebClient } from "./WebClientContext";
import { type ActivitySource, WorkspaceContext } from "./workspace-context";

const DEFAULT_SECONDARY_RATIO = 0.4;
const MIN_PRIMARY_WIDTH = 560;
const MIN_SECONDARY_WIDTH = 320;
const PANE_RATIO_STORAGE_KEY = "kit.web.workspace.secondaryRatio";
const RESIZE_STEP = 0.05;
const DIVIDER_WIDTH = 5;

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

function clampRatio(ratio: number): number {
	if (!Number.isFinite(ratio)) return DEFAULT_SECONDARY_RATIO;
	return Math.max(0.1, Math.min(0.9, ratio));
}

export function WorkspacePaneHost(props: {
	primary: JSX.Element;
	dock: JSX.Element;
}): JSX.Element {
	const { snapshot, transcriptItems } = useWebClient();
	const scratchpad = useScratchpad();
	const codeReview = useCodeReview();
	let host: HTMLElement | undefined;
	let returnFocus: HTMLElement | null = null;
	let activePointerId: number | null = null;
	const [hostWidth, setHostWidth] = createSignal(0);
	const [activitySource, setActivitySource] =
		createSignal<ActivitySource | null>(null);
	const [narrowTab, setNarrowTab] = createSignal<"transcript" | "secondary">(
		"transcript",
	);
	const [secondaryRatio, setSecondaryRatio] = createSignal(
		DEFAULT_SECONDARY_RATIO,
	);
	const [dragging, setDragging] = createSignal(false);
	const [focusedSurface, setFocusedSurface] = createSignal<
		"transcript" | "secondary"
	>("transcript");
	const [lastFocusedChrome, setLastFocusedChrome] = createSignal<
		"divider" | "tab" | null
	>(null);
	const panelOpen = createMemo(
		() => codeReview.open() || scratchpad.open() || activitySource() !== null,
	);
	const secondaryLabel = createMemo(() =>
		codeReview.open()
			? "Code review"
			: scratchpad.open()
				? "Scratchpad"
				: "Activity",
	);
	const split = createMemo(
		() =>
			panelOpen() &&
			hostWidth() >= MIN_PRIMARY_WIDTH + MIN_SECONDARY_WIDTH + DIVIDER_WIDTH,
	);
	const narrow = createMemo(() => panelOpen() && !split());
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
		if (persist) {
			localStorage.setItem(PANE_RATIO_STORAGE_KEY, String(constrained));
		}
	}

	function ratioAt(clientX: number): number {
		const bounds = host?.getBoundingClientRect();
		if (!bounds || bounds.width <= 0) return secondaryRatio();
		return (bounds.right - clientX - DIVIDER_WIDTH / 2) / bounds.width;
	}

	function finishPointerResize(event: PointerEvent, persist: boolean): void {
		if (activePointerId !== event.pointerId) return;
		const target = event.currentTarget;
		activePointerId = null;
		setDragging(false);
		if (persist) setRatio(ratioAt(event.clientX), true);
		if (
			target instanceof Element &&
			target.hasPointerCapture(event.pointerId)
		) {
			target.releasePointerCapture(event.pointerId);
		}
	}

	function openActivity(source: ActivitySource): void {
		codeReview.close();
		scratchpad.close();
		setActivitySource(source);
		setNarrowTab("secondary");
	}

	function isActivityOpen(source: ActivitySource): boolean {
		const active = activitySource();
		if (active === null) return false;
		if (activitySourcesMatch(active, source)) return true;
		if (
			active.kind !== "turn-intermediate" ||
			source.kind !== "turn-intermediate" ||
			active.turnId !== source.turnId
		) {
			return false;
		}
		return findTurnWorkItems(
			transcriptItems(),
			active.turnId,
			snapshot().protocol.activeTurnId,
			active.anchorItemId,
		).some((item) => item.id === source.anchorItemId);
	}

	function closeSecondary(): void {
		if (codeReview.open()) codeReview.close();
		else if (scratchpad.open()) scratchpad.close();
		else setActivitySource(null);
		setNarrowTab("transcript");
	}

	function selectRelativeTab(direction: -1 | 1): void {
		const tabs = ["transcript", "secondary"] as const;
		const current = tabs.indexOf(narrowTab());
		setNarrowTab(tabs[(current + direction + tabs.length) % tabs.length]);
	}

	function focusSelectedTab(): void {
		queueMicrotask(() =>
			host?.querySelector<HTMLElement>('[role="tab"][tabindex="0"]')?.focus(),
		);
	}

	function handleTabKeyDown(event: KeyboardEvent): void {
		if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
			event.preventDefault();
			selectRelativeTab(event.key === "ArrowLeft" ? -1 : 1);
			focusSelectedTab();
		} else if (event.key === "Home" || event.key === "End") {
			event.preventDefault();
			setNarrowTab(event.key === "Home" ? "transcript" : "secondary");
			focusSelectedTab();
		}
	}

	let previousPanelOpen = false;
	createEffect(() => {
		const currentPanelOpen = panelOpen();
		if (codeReview.open()) {
			scratchpad.close();
			setActivitySource(null);
			setNarrowTab("secondary");
		} else if (scratchpad.open()) {
			setActivitySource(null);
			setNarrowTab("secondary");
		}
		if (currentPanelOpen && !previousPanelOpen) {
			if (
				document.activeElement instanceof HTMLElement &&
				!document.activeElement.closest(".workspace-secondary")
			) {
				returnFocus = document.activeElement;
			}
		} else if (!currentPanelOpen && previousPanelOpen) {
			const target = returnFocus;
			returnFocus = null;
			queueMicrotask(() => {
				if (target?.isConnected) target.focus();
				else document.querySelector<HTMLElement>("#transcript")?.focus();
			});
		}
		previousPanelOpen = currentPanelOpen;
	});

	let observedSessionId: unknown;
	createEffect(() => {
		const sessionId = snapshot().protocol.serverState.sessionId;
		if (observedSessionId !== undefined && sessionId !== observedSessionId) {
			const focusNeedsRestore =
				document.activeElement instanceof Element &&
				document.activeElement.closest(
					".workspace-secondary, .workspace-tabs, .workspace-divider",
				) !== null;
			setActivitySource(null);
			setNarrowTab("transcript");
			returnFocus = null;
			if (focusNeedsRestore) {
				queueMicrotask(() =>
					document.querySelector<HTMLElement>("#transcript")?.focus(),
				);
			}
		}
		observedSessionId = sessionId;
	});

	let previousLayout: "single" | "split" | "tabs" = "single";
	createEffect(() => {
		const currentLayout = split() ? "split" : narrow() ? "tabs" : "single";
		if (previousLayout === "split" && currentLayout === "tabs") {
			setNarrowTab(focusedSurface());
			if (lastFocusedChrome() === "divider") focusSelectedTab();
		} else if (
			previousLayout === "tabs" &&
			currentLayout === "split" &&
			lastFocusedChrome() === "tab"
		) {
			queueMicrotask(() => {
				const selector =
					narrowTab() === "secondary"
						? scratchpad.open()
							? ".scratchpad-editor"
							: ".turn-activity-panel"
						: "#transcript";
				host?.querySelector<HTMLElement>(selector)?.focus();
			});
		}
		if (currentLayout !== "split" && dragging()) {
			activePointerId = null;
			setDragging(false);
		}
		previousLayout = currentLayout;
	});

	let resizeObserver: ResizeObserver | undefined;
	onMount(() => {
		const storedRatio = Number(localStorage.getItem(PANE_RATIO_STORAGE_KEY));
		if (Number.isFinite(storedRatio) && storedRatio > 0) {
			setSecondaryRatio(clampRatio(storedRatio));
		}
		if (!host) return;
		resizeObserver = new ResizeObserver(([entry]) => {
			if (entry) setHostWidth(entry.contentRect.width);
		});
		resizeObserver.observe(host);
	});
	onCleanup(() => resizeObserver?.disconnect());

	return (
		<WorkspaceContext.Provider value={{ openActivity, isActivityOpen }}>
			<section
				ref={host}
				class="workspace-pane-host"
				classList={{ "is-dragging": dragging() }}
				data-layout={split() ? "split" : narrow() ? "tabs" : "single"}
				style={{ "--workspace-divider-width": `${DIVIDER_WIDTH}px` }}
				aria-label="Session workspace"
				onFocusIn={(event) => {
					const target = event.target;
					if (!(target instanceof Element)) return;
					if (target.closest(".workspace-primary")) {
						setFocusedSurface("transcript");
						setLastFocusedChrome(null);
					} else if (target.closest(".workspace-secondary")) {
						setFocusedSurface("secondary");
						setLastFocusedChrome(null);
					}
				}}
				onKeyDown={(event) => {
					if (event.key !== "Escape" || !panelOpen()) return;
					event.preventDefault();
					closeSecondary();
				}}
			>
				<Show when={narrow()}>
					<nav
						class="workspace-tabs"
						role="tablist"
						aria-label="Workspace views"
					>
						<button
							id="workspace-tab-transcript"
							type="button"
							role="tab"
							data-variant="ghost"
							aria-selected={narrowTab() === "transcript"}
							aria-controls="workspace-transcript"
							tabIndex={narrowTab() === "transcript" ? 0 : -1}
							onClick={() => setNarrowTab("transcript")}
							onFocus={() => setLastFocusedChrome("tab")}
							onKeyDown={handleTabKeyDown}
						>
							Transcript
						</button>
						<button
							id="workspace-tab-secondary"
							type="button"
							role="tab"
							data-variant="ghost"
							aria-selected={narrowTab() === "secondary"}
							aria-controls="workspace-secondary"
							tabIndex={narrowTab() === "secondary" ? 0 : -1}
							onClick={() => setNarrowTab("secondary")}
							onFocus={() => setLastFocusedChrome("tab")}
							onKeyDown={handleTabKeyDown}
						>
							{secondaryLabel()}
						</button>
					</nav>
				</Show>
				<div class="workspace-primary">
					<div
						id="workspace-transcript"
						class="workspace-primary-content"
						role={narrow() ? "tabpanel" : undefined}
						aria-labelledby={narrow() ? "workspace-tab-transcript" : undefined}
						aria-hidden={narrow() && narrowTab() !== "transcript"}
						hidden={narrow() && narrowTab() !== "transcript"}
					>
						{props.primary}
					</div>
					<div
						class="workspace-dock"
						hidden={
							narrow() && narrowTab() === "secondary" && codeReview.open()
						}
					>
						{props.dock}
					</div>
				</div>
				<Show when={split()}>
					<div
						class="workspace-divider"
						classList={{ "is-dragging": dragging() }}
						role="separator"
						aria-label={`Resize ${secondaryLabel().toLowerCase()} panel`}
						aria-orientation="vertical"
						aria-valuemin={Math.round(minRatio() * 100)}
						aria-valuemax={Math.round(maxRatio() * 100)}
						aria-valuenow={Math.round(effectiveRatio() * 100)}
						tabIndex={0}
						onFocus={() => setLastFocusedChrome("divider")}
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
							setDragging(false);
						}}
					/>
				</Show>
				<Show when={panelOpen()}>
					<aside
						id="workspace-secondary"
						class="workspace-secondary"
						role={narrow() ? "tabpanel" : undefined}
						aria-label={narrow() ? undefined : secondaryLabel()}
						aria-labelledby={narrow() ? "workspace-tab-secondary" : undefined}
						aria-hidden={narrow() && narrowTab() !== "secondary"}
						hidden={narrow() && narrowTab() !== "secondary"}
						style={{ "--secondary-width": `${secondaryWidth()}px` }}
					>
						<Show
							when={codeReview.open()}
							fallback={
								<Show
									when={scratchpad.open()}
									fallback={
										<Show when={activitySource()}>
											{(source) => (
												<TurnActivityPanel
													source={source()}
													onClose={closeSecondary}
												/>
											)}
										</Show>
									}
								>
									<ScratchpadPanel />
								</Show>
							}
						>
							<CodeReviewPanel />
						</Show>
					</aside>
				</Show>
			</section>
		</WorkspaceContext.Provider>
	);
}
