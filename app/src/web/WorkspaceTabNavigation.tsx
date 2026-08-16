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
import {
	CHEVRON_LEFT,
	CHEVRON_RIGHT,
	GUILLEMET_RIGHT,
	TIMES,
} from "../shell/glyphs";
import { DialogFrame } from "./DialogFrame";
import {
	directTabsForCount,
	paneClosable,
	paneLabel,
	type WebWorkspaceTab,
} from "./workspace-panes";

export function focusWorkspaceTab(
	host: ParentNode | undefined,
	tabId: string,
): void {
	queueMicrotask(() =>
		host
			?.querySelector<HTMLElement>(`[role="tab"][data-tab-id="${tabId}"]`)
			?.focus(),
	);
}

function WorkspaceTab(props: {
	tab: WebWorkspaceTab;
	selected: boolean;
	elementRef?: (element: HTMLDivElement) => void;
	measurement?: boolean;
	controls: string;
	onSelect: () => void;
	onClose?: () => void;
	onKeyDown: (event: KeyboardEvent) => void;
}): JSX.Element {
	return (
		<div
			ref={props.elementRef}
			role="presentation"
			class="workspace-tab-item"
			classList={{ "is-selected": props.selected }}
		>
			<button
				id={props.measurement ? undefined : `workspace-tab-${props.tab.id}`}
				type="button"
				role={props.measurement ? undefined : "tab"}
				class="workspace-tab-label"
				data-variant="ghost"
				data-tab-id={props.measurement ? undefined : props.tab.id}
				aria-selected={props.measurement ? undefined : props.selected}
				aria-controls={props.measurement ? undefined : props.controls}
				tabIndex={props.measurement ? -1 : props.selected ? 0 : -1}
				onClick={props.onSelect}
				onKeyDown={props.onKeyDown}
			>
				<span>{paneLabel(props.tab.pane)}</span>
			</button>
			<Show when={props.onClose !== undefined}>
				<button
					type="button"
					class="workspace-tab-close"
					data-variant="ghost"
					aria-label={
						props.measurement ? undefined : `Close ${paneLabel(props.tab.pane)}`
					}
					title={
						props.measurement ? undefined : `Close ${paneLabel(props.tab.pane)}`
					}
					tabIndex={props.measurement ? -1 : 0}
					onClick={(event) => {
						event.stopPropagation();
						props.onClose?.();
					}}
				>
					{TIMES}
				</button>
			</Show>
		</div>
	);
}

export function DesktopWorkspaceTabs(props: {
	tabs: readonly WebWorkspaceTab[];
	activeTabId: string;
	onSelect: (tabId: string) => void;
	onClose: (tabId: string) => void;
	onCollapse: () => void;
}): JSX.Element {
	let track: HTMLDivElement | undefined;
	const [overflowStart, setOverflowStart] = createSignal(false);
	const [overflowEnd, setOverflowEnd] = createSignal(false);

	function syncOverflow(): void {
		if (!track) return;
		setOverflowStart(track.scrollLeft > 1);
		setOverflowEnd(
			track.scrollLeft + track.clientWidth < track.scrollWidth - 1,
		);
	}

	function reveal(tabId: string): void {
		queueMicrotask(() => {
			track
				?.querySelector<HTMLElement>(`[data-tab-id="${tabId}"]`)
				?.scrollIntoView({ block: "nearest", inline: "nearest" });
			syncOverflow();
		});
	}

	function select(tabId: string): void {
		props.onSelect(tabId);
		reveal(tabId);
	}

	function handleKeyDown(tabId: string, event: KeyboardEvent): void {
		const index = props.tabs.findIndex((tab) => tab.id === tabId);
		if (index < 0) return;
		let next: WebWorkspaceTab | undefined;
		if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
			event.preventDefault();
			const direction = event.key === "ArrowLeft" ? -1 : 1;
			next =
				props.tabs[(index + direction + props.tabs.length) % props.tabs.length];
		} else if (event.key === "Home" || event.key === "End") {
			event.preventDefault();
			next = event.key === "Home" ? props.tabs[0] : props.tabs.at(-1);
		}
		if (!next) return;
		select(next.id);
		focusWorkspaceTab(track, next.id);
	}

	let observer: ResizeObserver | undefined;
	onMount(() => {
		observer = new ResizeObserver(syncOverflow);
		if (track) observer.observe(track);
		syncOverflow();
	});
	onCleanup(() => observer?.disconnect());
	createEffect(() => {
		void props.tabs.length;
		void props.activeTabId;
		reveal(props.activeTabId);
	});

	return (
		<nav class="workspace-drawer-tabs" aria-label="Workspace views">
			<button
				type="button"
				class="workspace-tab-edge"
				data-variant="ghost"
				aria-label="Scroll workspace tabs left"
				disabled={!overflowStart()}
				onClick={() => track?.scrollBy({ left: -160, behavior: "auto" })}
			>
				{CHEVRON_LEFT}
			</button>
			<div
				ref={track}
				class="workspace-tab-track"
				role="tablist"
				aria-label="Workspace views"
				onScroll={syncOverflow}
				onWheel={(event) => {
					if (!track || Math.abs(event.deltaY) <= Math.abs(event.deltaX))
						return;
					event.preventDefault();
					track.scrollLeft += event.deltaY;
				}}
			>
				<For each={props.tabs}>
					{(tab) => (
						<WorkspaceTab
							tab={tab}
							selected={tab.id === props.activeTabId}
							controls={`workspace-pane-${tab.id}`}
							onSelect={() => select(tab.id)}
							onClose={
								paneClosable(tab.pane) ? () => props.onClose(tab.id) : undefined
							}
							onKeyDown={(event) => handleKeyDown(tab.id, event)}
						/>
					)}
				</For>
			</div>
			<button
				type="button"
				class="workspace-tab-edge"
				data-variant="ghost"
				aria-label="Scroll workspace tabs right"
				disabled={!overflowEnd()}
				onClick={() => track?.scrollBy({ left: 160, behavior: "auto" })}
			>
				{CHEVRON_RIGHT}
			</button>
			<button
				type="button"
				class="workspace-collapse"
				data-variant="ghost"
				aria-label="Collapse workspace drawer"
				title="Collapse workspace drawer"
				onClick={props.onCollapse}
			>
				{GUILLEMET_RIGHT}
			</button>
		</nav>
	);
}

function WorkspaceSurfaceSheet(props: {
	open: boolean;
	focusFilter: boolean;
	tabs: readonly WebWorkspaceTab[];
	onClose: () => void;
	onSelect: (tabId: string) => void;
	onCloseTab: (tabId: string) => void;
}): JSX.Element {
	const [query, setQuery] = createSignal("");
	const filtered = createMemo(() => {
		const normalized = query().trim().toLowerCase();
		return normalized
			? props.tabs.filter((tab) =>
					paneLabel(tab.pane).toLowerCase().includes(normalized),
				)
			: props.tabs;
	});
	createEffect(() => {
		if (!props.open) setQuery("");
	});
	return (
		<DialogFrame
			open={props.open}
			id="workspace-surface-sheet"
			class="workspace-surface-sheet"
			labelledBy="workspace-surface-sheet-title"
			drawer
			onCancel={props.onClose}
			onAfterOpen={(dialog) => {
				if (props.focusFilter)
					dialog.querySelector<HTMLInputElement>("input")?.focus();
				else
					dialog
						.querySelector<HTMLElement>(".workspace-surface-option button")
						?.focus();
			}}
		>
			<header>
				<h2 id="workspace-surface-sheet-title">Workspace surfaces</h2>
				<button
					type="button"
					data-variant="ghost"
					aria-label="Close workspace surfaces"
					onClick={props.onClose}
				>
					{TIMES}
				</button>
			</header>
			<input
				type="search"
				value={query()}
				placeholder="Filter surfaces…"
				aria-label="Filter workspace surfaces"
				onInput={(event) => setQuery(event.currentTarget.value)}
			/>
			<div class="workspace-surface-options">
				<For each={filtered()}>
					{(tab) => (
						<div class="workspace-surface-option">
							<button
								type="button"
								data-variant="ghost"
								onClick={() => {
									props.onSelect(tab.id);
									props.onClose();
								}}
							>
								{paneLabel(tab.pane)}
							</button>
							<Show when={paneClosable(tab.pane)}>
								<button
									type="button"
									data-variant="ghost"
									aria-label={`Close ${paneLabel(tab.pane)}`}
									onClick={() => {
										props.onCloseTab(tab.id);
										queueMicrotask(() =>
											document
												.querySelector<HTMLElement>(
													"#workspace-surface-sheet input, #workspace-surface-sheet .workspace-surface-option button",
												)
												?.focus(),
										);
									}}
								>
									{TIMES}
								</button>
							</Show>
						</div>
					)}
				</For>
			</div>
		</DialogFrame>
	);
}

export function NarrowWorkspaceTabs(props: {
	tabs: readonly WebWorkspaceTab[];
	selectedId: string;
	onSelectTranscript: () => void;
	onSelect: (tabId: string) => void;
	onClose: (tabId: string, restoreFocus?: boolean) => void;
}): JSX.Element {
	let nav: HTMLElement | undefined;
	let transcriptMeasure: HTMLButtonElement | undefined;
	let moreMeasure: HTMLButtonElement | undefined;
	let measureList: HTMLDivElement | undefined;
	const tabMeasures = new Map<string, HTMLDivElement>();
	const [visibleCount, setVisibleCount] = createSignal(props.tabs.length);
	const [sheetOpen, setSheetOpen] = createSignal(false);
	const [sheetOpenedWithKeyboard, setSheetOpenedWithKeyboard] =
		createSignal(false);
	const visibleTabs = createMemo(() =>
		directTabsForCount(props.tabs, visibleCount(), props.selectedId),
	);
	const overflowTabs = createMemo(() => {
		const visibleIds = new Set(visibleTabs().map((tab) => tab.id));
		return props.tabs.filter((tab) => !visibleIds.has(tab.id));
	});

	function measure(): void {
		if (!nav || !measureList || !transcriptMeasure) return;
		const navStyle = getComputedStyle(nav);
		const available =
			nav.clientWidth -
			parseFloat(navStyle.paddingLeft || "0") -
			parseFloat(navStyle.paddingRight || "0");
		const gap = parseFloat(getComputedStyle(measureList).columnGap || "0");
		const base = transcriptMeasure.getBoundingClientRect().width;
		const widthOf = (tab: WebWorkspaceTab) =>
			tabMeasures.get(tab.id)?.getBoundingClientRect().width ?? 112;
		const allWidth =
			base +
			props.tabs.reduce((sum, tab) => sum + widthOf(tab), 0) +
			gap * props.tabs.length;
		if (allWidth <= available) {
			setVisibleCount(props.tabs.length);
			return;
		}
		const moreWidth = moreMeasure?.getBoundingClientRect().width ?? 52;
		for (let count = props.tabs.length - 1; count >= 0; count -= 1) {
			const direct = directTabsForCount(props.tabs, count, props.selectedId);
			const used =
				base +
				moreWidth +
				direct.reduce((sum, tab) => sum + widthOf(tab), 0) +
				gap * (direct.length + 1);
			if (used <= available) {
				setVisibleCount(count);
				return;
			}
		}
		setVisibleCount(0);
	}

	function handleKeyDown(tabId: string, event: KeyboardEvent): void {
		const ids = ["transcript", ...visibleTabs().map((tab) => tab.id)];
		const index = ids.indexOf(tabId);
		if (index < 0) return;
		let nextId: string | undefined;
		if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
			event.preventDefault();
			const direction = event.key === "ArrowLeft" ? -1 : 1;
			nextId = ids[(index + direction + ids.length) % ids.length];
		} else if (event.key === "Home" || event.key === "End") {
			event.preventDefault();
			nextId = event.key === "Home" ? ids[0] : ids.at(-1);
		}
		if (!nextId) return;
		if (nextId === "transcript") props.onSelectTranscript();
		else props.onSelect(nextId);
		focusWorkspaceTab(nav, nextId);
	}

	let observer: ResizeObserver | undefined;
	onMount(() => {
		observer = new ResizeObserver(measure);
		if (nav) observer.observe(nav);
		measure();
	});
	onCleanup(() => observer?.disconnect());
	createEffect(() => {
		void props.tabs.length;
		void props.selectedId;
		queueMicrotask(measure);
	});
	createEffect(() => {
		if (sheetOpen() && overflowTabs().length === 0) setSheetOpen(false);
	});

	return (
		<>
			<nav ref={nav} class="workspace-tabs" aria-label="Workspace views">
				<div
					ref={measureList}
					class="workspace-tab-measure workspace-narrow-tablist"
					aria-hidden="true"
				>
					<button
						ref={transcriptMeasure}
						class="workspace-tab-measure-transcript"
						tabIndex={-1}
					>
						Transcript
					</button>
					<For each={props.tabs}>
						{(tab) => (
							<WorkspaceTab
								tab={tab}
								selected={false}
								measurement
								controls=""
								elementRef={(element) => tabMeasures.set(tab.id, element)}
								onSelect={() => {}}
								onClose={paneClosable(tab.pane) ? () => {} : undefined}
								onKeyDown={() => {}}
							/>
						)}
					</For>
					<button ref={moreMeasure} class="workspace-more" tabIndex={-1}>
						+99
					</button>
				</div>
				<div
					class="workspace-narrow-tablist"
					role="tablist"
					aria-label="Workspace views"
				>
					<button
						id="workspace-tab-transcript"
						type="button"
						role="tab"
						data-variant="ghost"
						data-tab-id="transcript"
						aria-selected={props.selectedId === "transcript"}
						aria-controls="workspace-transcript"
						tabIndex={props.selectedId === "transcript" ? 0 : -1}
						onClick={props.onSelectTranscript}
						onKeyDown={(event) => handleKeyDown("transcript", event)}
					>
						Transcript
					</button>
					<For each={visibleTabs()}>
						{(tab) => (
							<WorkspaceTab
								tab={tab}
								selected={props.selectedId === tab.id}
								controls={`workspace-pane-${tab.id}`}
								onSelect={() => props.onSelect(tab.id)}
								onClose={
									paneClosable(tab.pane)
										? () => props.onClose(tab.id)
										: undefined
								}
								onKeyDown={(event) => handleKeyDown(tab.id, event)}
							/>
						)}
					</For>
				</div>
				<Show when={overflowTabs().length > 0}>
					<button
						type="button"
						class="workspace-more"
						data-variant="ghost"
						aria-label={`Show ${overflowTabs().length} more workspace ${overflowTabs().length === 1 ? "surface" : "surfaces"}`}
						aria-haspopup="dialog"
						aria-expanded={sheetOpen()}
						onPointerDown={() => setSheetOpenedWithKeyboard(false)}
						onKeyDown={(event) => {
							if (event.key === "Enter" || event.key === " ") {
								setSheetOpenedWithKeyboard(true);
							}
						}}
						onClick={() => setSheetOpen(true)}
					>
						+{overflowTabs().length}
					</button>
				</Show>
			</nav>
			<WorkspaceSurfaceSheet
				open={sheetOpen()}
				focusFilter={sheetOpenedWithKeyboard()}
				tabs={overflowTabs()}
				onClose={() => setSheetOpen(false)}
				onSelect={props.onSelect}
				onCloseTab={(tabId) => props.onClose(tabId, false)}
			/>
		</>
	);
}
