import type { MouseEvent } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createSignal,
	onCleanup,
	untrack,
} from "solid-js";
import { CHEVRON_LEFT, CHEVRON_RIGHT, TIMES } from "./glyphs";
import { theme } from "./theme";
import {
	resolveNarrowWorkspaceTabs,
	resolveWideWorkspaceTabs,
	revealWideWorkspaceTab,
	type WorkspaceTabItem,
	type WorkspaceVisibleTab,
} from "./workspace-tabs-layout";

export type WorkspaceTabMouseHandler = (event: MouseEvent) => boolean;

export type WorkspaceTabStripProps = {
	mode: "wide" | "narrow";
	visible?: () => boolean;
	width: () => number;
	tabs: () => readonly WorkspaceTabItem[];
	activeTabId: () => string;
	selectedSurface: () => "transcript" | string;
	onSelectTranscript: () => void;
	onSelectTab: (tabId: string) => void;
	onCloseTab: (tabId: string) => void;
	onCollapse: () => void;
	onOpenOverflow: (tabs: readonly WorkspaceTabItem[]) => void;
	onMouseHandlerReady?: (handler: WorkspaceTabMouseHandler | null) => void;
	shouldHandleMouseEvent?: (event: MouseEvent) => boolean;
};

const PRIMARY_MOUSE_BUTTON = 0;

type TabAction =
	| { kind: "select"; id: string }
	| { kind: "close"; id: string }
	| { kind: "scroll"; direction: -1 | 1 }
	| { kind: "collapse" }
	| { kind: "overflow" };

export function WorkspaceTabStrip(props: WorkspaceTabStripProps) {
	const renderer = useRenderer();
	let stripRef: { screenX: number } | undefined;
	const [offset, setOffset] = createSignal(0);
	const wideLayout = createMemo(() =>
		resolveWideWorkspaceTabs({
			tabs: props.tabs(),
			width: props.width(),
			offset: offset(),
		}),
	);
	const narrowLayout = createMemo(() =>
		resolveNarrowWorkspaceTabs({ tabs: props.tabs(), width: props.width() }),
	);

	let revealedSignature = "";
	createEffect(() => {
		if (props.mode !== "wide") return;
		const signature = `${props.activeTabId()}\u0000${props.width()}\u0000${props
			.tabs()
			.map((tab) => tab.id)
			.join("\u0000")}`;
		if (signature === revealedSignature) return;
		revealedSignature = signature;
		const currentOffset = untrack(offset);
		const revealed = revealWideWorkspaceTab({
			tabs: props.tabs(),
			width: props.width(),
			offset: currentOffset,
			activeTabId: props.activeTabId(),
		});
		if (revealed !== currentOffset) setOffset(revealed);
	});

	createEffect(() => {
		const maxOffset = Math.max(0, props.tabs().length - 1);
		if (offset() > maxOffset) setOffset(maxOffset);
	});

	onCleanup(() => renderer.setMousePointer("default"));

	function consume(event: MouseEvent): void {
		event.preventDefault();
		event.stopPropagation();
	}

	function activate(action: TabAction, event: MouseEvent): void {
		if (event.button !== PRIMARY_MOUSE_BUTTON) return;
		consume(event);
		switch (action.kind) {
			case "select":
				if (action.id === "transcript") props.onSelectTranscript();
				else props.onSelectTab(action.id);
				break;
			case "close":
				props.onCloseTab(action.id);
				break;
			case "scroll":
				setOffset((current) =>
					Math.max(
						0,
						Math.min(props.tabs().length - 1, current + action.direction),
					),
				);
				break;
			case "collapse":
				props.onCollapse();
				break;
			case "overflow":
				props.onOpenOverflow(narrowLayout().overflow);
				break;
		}
	}

	function activateVisibleTab(
		items: readonly WorkspaceVisibleTab[],
		localX: number,
		startX: number,
		event: MouseEvent,
	): boolean {
		let tabX = startX;
		for (const item of items) {
			if (localX >= tabX && localX < tabX + item.width) {
				const inCloseAction =
					item.closable !== false && localX >= tabX + item.width - 2;
				activate(
					inCloseAction
						? { kind: "close", id: item.id }
						: { kind: "select", id: item.id },
					event,
				);
				return true;
			}
			tabX += item.width;
		}
		return false;
	}

	function handleStripMouseDown(event: MouseEvent): boolean {
		// WorkspacePaneHost accepts only its marked raw-input event here. This
		// prevents duplicate actions if OpenTUI also dispatches the same press.
		if (
			event.button !== PRIMARY_MOUSE_BUTTON ||
			(props.shouldHandleMouseEvent && !props.shouldHandleMouseEvent(event))
		) {
			return false;
		}
		const localX = event.x - (stripRef?.screenX ?? 0);
		if (props.mode === "narrow") {
			if (activateVisibleTab(narrowLayout().visible, localX, 0, event)) {
				return true;
			}
			if (narrowLayout().overflow.length === 0) return false;
			activate({ kind: "overflow" }, event);
			return true;
		}
		const beforeWidth =
			wideLayout().hiddenBefore > 0
				? String(wideLayout().hiddenBefore).length + 2
				: 0;
		if (localX < beforeWidth) {
			activate({ kind: "scroll", direction: -1 }, event);
			return true;
		}
		if (activateVisibleTab(wideLayout().visible, localX, beforeWidth, event)) {
			return true;
		}
		if (localX >= props.width() - 2) {
			activate({ kind: "collapse" }, event);
			return true;
		}
		const afterWidth =
			wideLayout().hiddenAfter > 0
				? String(wideLayout().hiddenAfter).length + 2
				: 0;
		if (afterWidth > 0 && localX >= props.width() - 2 - afterWidth) {
			activate({ kind: "scroll", direction: 1 }, event);
			return true;
		}
		return false;
	}

	props.onMouseHandlerReady?.(handleStripMouseDown);
	onCleanup(() => props.onMouseHandlerReady?.(null));

	function pointerEnter(): void {
		renderer.setMousePointer("pointer");
	}

	function pointerLeave(): void {
		renderer.setMousePointer("default");
	}

	function Tab(tabProps: { item: WorkspaceVisibleTab; selected: boolean }) {
		const item = tabProps.item;
		const closeWidth = item.closable === false ? 0 : 2;
		return (
			<box
				width={item.width}
				height={1}
				flexShrink={0}
				flexDirection="row"
				zIndex={1}
				backgroundColor={theme.bg}
			>
				<text
					width={item.width - closeWidth}
					fg={tabProps.selected ? theme.textPrimary : theme.textMuted}
					onMouseOver={pointerEnter}
					onMouseOut={pointerLeave}
				>
					{` ${item.displayLabel} `}
				</text>
				{item.closable === false ? null : (
					<text
						width={2}
						fg={tabProps.selected ? theme.textSecondary : theme.textMuted}
						onMouseOver={pointerEnter}
						onMouseOut={pointerLeave}
					>
						{` ${TIMES}`}
					</text>
				)}
			</box>
		);
	}

	if (props.mode === "narrow") {
		return (
			<box
				visible={props.visible?.() !== false}
				width="100%"
				height={2}
				flexShrink={0}
				flexDirection="column"
				overflow="hidden"
				backgroundColor={theme.bg}
			>
				<box
					width="100%"
					height={1}
					flexShrink={0}
					flexDirection="row"
					alignItems="center"
					backgroundColor={theme.bg}
					ref={(value) => {
						stripRef = value;
					}}
					onMouseDown={handleStripMouseDown}
				>
					{narrowLayout().visible.map((item) => (
						<Tab item={item} selected={props.selectedSurface() === item.id} />
					))}
					{narrowLayout().overflow.length > 0 ? (
						<box
							flexGrow={1}
							minWidth={3}
							justifyContent="flex-end"
							paddingRight={1}
							onMouseOver={pointerEnter}
							onMouseOut={pointerLeave}
						>
							<text fg={theme.metaText}>+{narrowLayout().overflow.length}</text>
						</box>
					) : (
						<box width={0} />
					)}
				</box>
				<box
					width="100%"
					height={1}
					flexShrink={0}
					border={["top"]}
					borderColor={theme.borderDefault}
				/>
			</box>
		);
	}

	return (
		<box
			visible={props.visible?.() !== false}
			width="100%"
			height={2}
			flexShrink={0}
			flexDirection="column"
			overflow="hidden"
			backgroundColor={theme.bg}
		>
			<box
				width="100%"
				height={1}
				flexShrink={0}
				flexDirection="row"
				alignItems="center"
				backgroundColor={theme.bg}
				ref={(value) => {
					stripRef = value;
				}}
				onMouseDown={handleStripMouseDown}
				onMouseScroll={(event) => {
					consume(event);
					if (event.button === 4)
						setOffset((current) => Math.max(0, current - 1));
					if (event.button === 5) {
						setOffset((current) =>
							Math.min(Math.max(0, props.tabs().length - 1), current + 1),
						);
					}
				}}
			>
				{wideLayout().hiddenBefore > 0 ? (
					<box
						width={String(wideLayout().hiddenBefore).length + 2}
						flexShrink={0}
						onMouseOver={pointerEnter}
						onMouseOut={pointerLeave}
					>
						<text fg={theme.metaText}>
							{CHEVRON_LEFT} {wideLayout().hiddenBefore}
						</text>
					</box>
				) : (
					<box width={0} />
				)}
				{wideLayout().visible.map((item) => (
					<Tab item={item} selected={props.activeTabId() === item.id} />
				))}
				<box flexGrow={1} />
				{wideLayout().hiddenAfter > 0 ? (
					<box
						width={String(wideLayout().hiddenAfter).length + 2}
						flexShrink={0}
						onMouseOver={pointerEnter}
						onMouseOut={pointerLeave}
					>
						<text fg={theme.metaText}>
							{wideLayout().hiddenAfter} {CHEVRON_RIGHT}
						</text>
					</box>
				) : (
					<box width={0} />
				)}
				<box
					width={2}
					flexShrink={0}
					justifyContent="center"
					onMouseOver={pointerEnter}
					onMouseOut={pointerLeave}
				>
					<text fg={theme.textSecondary}>{CHEVRON_RIGHT}</text>
				</box>
			</box>
			<box
				width="100%"
				height={1}
				flexShrink={0}
				border={["top"]}
				borderColor={theme.borderDefault}
			/>
		</box>
	);
}
