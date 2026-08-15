import { MouseEvent } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import type { JSX } from "solid-js";
import { createEffect, createMemo, createSignal, onCleanup } from "solid-js";
import { ANGLE_LEFT } from "./glyphs";
import { theme } from "./theme";
import {
	type WorkspaceTabMouseHandler,
	WorkspaceTabStrip,
} from "./WorkspaceTabStrip";
import {
	preferredPaneRatioFromDivider,
	resolveWorkspacePaneLayout,
} from "./workspace-layout";
import type { WorkspaceTabItem } from "./workspace-tabs-layout";

export type WorkspacePaneHostProps = {
	children: JSX.Element;
	secondary: () => JSX.Element;
	tabs: () => readonly WorkspaceTabItem[];
	activeTabId: () => string;
	selectedSurface: () => "transcript" | string;
	drawerCollapsed: () => boolean;
	initialWidth: number;
	preferredPaneRatio: () => number;
	minPrimaryColumns: number;
	minSecondaryColumns: () => number;
	onPreferredPaneRatioChange: (ratio: number) => void;
	onPreferredPaneRatioCommit: (ratio: number) => void;
	onDividerMouseDown?: () => void;
	onSelectTranscript: () => void;
	onSelectTab: (tabId: string) => void;
	onCloseTab: (tabId: string) => void;
	onCollapseDrawer: () => void;
	onExpandDrawer: () => void;
	onOpenOverflow: (tabs: readonly WorkspaceTabItem[]) => void;
};

const PRIMARY_MOUSE_BUTTON = 0;
const COLLAPSED_HANDLE_WIDTH = 3;

/**
 * Parse the terminal's SGR primary-button press so workspace tabs can bypass
 * OpenTUI's hit grid. In the full retained/responsive workspace tree, OpenTUI
 * paints the tab strip correctly but can omit it from mouse hit testing after
 * layout and visibility transitions. Keep this workaround scoped to presses;
 * all non-workspace mouse input continues through OpenTUI normally.
 */
export function parseSgrPrimaryMouseDown(sequence: string): MouseEvent | null {
	if (!sequence.startsWith("\x1b[<")) return null;
	const match = /^(\d+);(\d+);(\d+)M$/.exec(sequence.slice(3));
	if (!match) return null;
	const buttonCode = Number(match[1]);
	if (
		(buttonCode & 3) !== PRIMARY_MOUSE_BUTTON ||
		(buttonCode & 32) !== 0 ||
		(buttonCode & 64) !== 0
	) {
		return null;
	}
	return new MouseEvent(null, {
		type: "down",
		button: PRIMARY_MOUSE_BUTTON,
		x: Number(match[2]) - 1,
		y: Number(match[3]) - 1,
		modifiers: {
			shift: (buttonCode & 4) !== 0,
			alt: (buttonCode & 8) !== 0,
			ctrl: (buttonCode & 16) !== 0,
		},
	});
}

type HostRef = {
	width: number;
	screenX: number;
	screenY: number;
};

type HandleRef = {
	width: number;
	screenX: number;
};

export function WorkspacePaneHost(props: WorkspacePaneHostProps) {
	const renderer = useRenderer();
	const SecondarySurface = props.secondary;
	let hostRef: HostRef | undefined;
	let handleRef: HandleRef | undefined;
	let narrowTabMouseHandler: WorkspaceTabMouseHandler | null = null;
	let wideTabMouseHandler: WorkspaceTabMouseHandler | null = null;
	// Synthetic events are marked so the tab strip can reject a second dispatch
	// if OpenTUI also resolves the same raw press through its normal hit grid.
	const rawMouseEvents = new WeakSet<MouseEvent>();
	let lastHandledRawMouseDown: { x: number; y: number; at: number } | null =
		null;
	const hostWidth = () => props.initialWidth;
	const [dividerHovered, setDividerHovered] = createSignal(false);
	const [handleHovered, setHandleHovered] = createSignal(false);
	const [handlePressed, setHandlePressed] = createSignal(false);
	const [dragging, setDragging] = createSignal(false);
	const [dragRatio, setDragRatio] = createSignal<number | null>(null);
	const hasTabs = () => props.tabs().length > 0;
	const layout = createMemo(() => {
		if (!hasTabs()) return null;
		return resolveWorkspacePaneLayout({
			availableColumns: hostWidth(),
			preferredPaneRatio: dragRatio() ?? props.preferredPaneRatio(),
			minPrimaryColumns: props.minPrimaryColumns,
			minSecondaryColumns: props.minSecondaryColumns(),
		});
	});
	const narrowTabbed = () => hasTabs() && layout() === null;
	const wideCollapsed = () =>
		!hasTabs() || (layout() !== null && props.drawerCollapsed());
	const primaryVisible = () =>
		!narrowTabbed() || props.selectedSurface() === "transcript";
	const secondaryVisible = () =>
		(layout() !== null && !wideCollapsed()) ||
		(narrowTabbed() && props.selectedSurface() !== "transcript");

	function dragInvalidated(): boolean {
		return props.drawerCollapsed() || !hasTabs() || layout() === null;
	}

	function cancelDrag(): void {
		setDragRatio(null);
		setDragging(false);
		renderer.setMousePointer("default");
	}

	createEffect(() => {
		if (dragging() && dragInvalidated()) cancelDrag();
	});

	function shouldHandleTabMouseEvent(event: MouseEvent): boolean {
		if (rawMouseEvents.has(event)) return true;
		return !(
			lastHandledRawMouseDown &&
			Date.now() - lastHandledRawMouseDown.at < 100 &&
			event.x === lastHandledRawMouseDown.x &&
			event.y === lastHandledRawMouseDown.y
		);
	}

	function handleHostMouseDown(event: MouseEvent): boolean {
		if (
			!shouldHandleTabMouseEvent(event) ||
			event.defaultPrevented ||
			event.button !== PRIMARY_MOUSE_BUTTON ||
			!hostRef
		) {
			return false;
		}
		if (
			wideCollapsed() &&
			handleRef &&
			event.x >= handleRef.screenX &&
			event.x < handleRef.screenX + handleRef.width
		) {
			consumeMouse(event);
			props.onExpandDrawer();
			return true;
		}
		const localY = event.y - hostRef.screenY;
		// Use a coarse host-relative window here because the shell border/header can
		// offset OpenTUI's host geometry. WorkspaceTabStrip performs the definitive
		// check against its measured screenY and height before activating anything.
		if (localY < -1 || localY >= 4) return false;
		if (narrowTabbed()) {
			return narrowTabMouseHandler?.(event) ?? false;
		}
		const currentLayout = layout();
		if (
			!currentLayout ||
			wideCollapsed() ||
			event.x < hostRef.screenX + currentLayout.primaryColumns
		) {
			return false;
		}
		return wideTabMouseHandler?.(event) ?? false;
	}

	function ratioAt(event: MouseEvent): number {
		const preferredPaneRatio = preferredPaneRatioFromDivider({
			availableColumns: hostWidth(),
			containerLeft: hostRef?.screenX ?? 0,
			dividerX: event.x,
		});
		const constrained = resolveWorkspacePaneLayout({
			availableColumns: hostWidth(),
			preferredPaneRatio,
			minPrimaryColumns: props.minPrimaryColumns,
			minSecondaryColumns: props.minSecondaryColumns(),
		});
		return constrained
			? constrained.secondaryColumns / hostWidth()
			: preferredPaneRatio;
	}

	function consumeMouse(event: MouseEvent): void {
		event.preventDefault();
		event.stopPropagation();
	}

	function draggedToCollapsedWidth(event: MouseEvent): boolean {
		const preferredPaneRatio = preferredPaneRatioFromDivider({
			availableColumns: hostWidth(),
			containerLeft: hostRef?.screenX ?? 0,
			dividerX: event.x,
		});
		return (
			Math.round(hostWidth() * preferredPaneRatio) <=
			props.minSecondaryColumns()
		);
	}

	function collapseDraggedPane(event: MouseEvent): void {
		consumeMouse(event);
		cancelDrag();
		props.onCollapseDrawer();
	}

	function updateRatio(event: MouseEvent): void {
		if (dragInvalidated()) {
			consumeMouse(event);
			cancelDrag();
			return;
		}
		if (draggedToCollapsedWidth(event)) {
			collapseDraggedPane(event);
			return;
		}
		consumeMouse(event);
		setDragRatio(ratioAt(event));
	}

	function finishDrag(event: MouseEvent): void {
		consumeMouse(event);
		if (!dragging()) return;
		if (dragInvalidated()) {
			cancelDrag();
			return;
		}
		if (draggedToCollapsedWidth(event)) {
			collapseDraggedPane(event);
			return;
		}
		const ratio = ratioAt(event);
		props.onPreferredPaneRatioChange(ratio);
		props.onPreferredPaneRatioCommit(ratio);
		setDragRatio(null);
		setDragging(false);
		renderer.setMousePointer(dividerHovered() ? "move" : "default");
	}

	// Intentional low-level workaround: real PTY testing showed that raw SGR
	// mouse input arrived while no tab-strip or ancestor onMouseDown handler ran.
	// Route only presses inside workspace chrome through its measured geometry.
	// Do not replace this with JSX handlers until the full app works in a real
	// terminal; the isolated OpenTUI test renderer does not reproduce the bug.
	const rawStdinMouseHandler = (data: Buffer | string): void => {
		const input = typeof data === "string" ? data : data.toString("utf8");
		let start = input.indexOf("\x1b[<");
		while (start >= 0) {
			const end = input.indexOf("M", start + 3);
			if (end < 0) return;
			const event = parseSgrPrimaryMouseDown(input.slice(start, end + 1));
			if (event) {
				rawMouseEvents.add(event);
				if (handleHostMouseDown(event)) {
					lastHandledRawMouseDown = { x: event.x, y: event.y, at: Date.now() };
				}
			}
			start = input.indexOf("\x1b[<", end + 1);
		}
	};
	renderer.stdin.prependListener("data", rawStdinMouseHandler);
	onCleanup(() => renderer.stdin.removeListener("data", rawStdinMouseHandler));

	onCleanup(() => {
		if (dividerHovered() || dragging() || handleHovered()) {
			renderer.setMousePointer("default");
		}
	});

	return (
		<box
			position="relative"
			flexGrow={1}
			width="100%"
			height="100%"
			flexDirection="column"
			ref={(value) => {
				hostRef = value as HostRef;
			}}
			onMouseDown={handleHostMouseDown}
			onMouseDrag={(event) => {
				if (dragging()) updateRatio(event);
			}}
			onMouseDragEnd={(event) => {
				if (dragging()) finishDrag(event);
			}}
			onMouseUp={(event) => {
				if (dragging()) finishDrag(event);
			}}
		>
			<box
				position="relative"
				width="100%"
				height={narrowTabbed() ? 2 : 0}
				flexShrink={0}
				overflow="hidden"
			>
				<WorkspaceTabStrip
					mode="narrow"
					visible={narrowTabbed}
					width={hostWidth}
					tabs={props.tabs}
					activeTabId={props.activeTabId}
					selectedSurface={props.selectedSurface}
					onSelectTranscript={props.onSelectTranscript}
					onSelectTab={props.onSelectTab}
					onCloseTab={props.onCloseTab}
					onCollapse={props.onCollapseDrawer}
					onOpenOverflow={props.onOpenOverflow}
					onMouseHandlerReady={(handler) => {
						narrowTabMouseHandler = handler;
					}}
					shouldHandleMouseEvent={shouldHandleTabMouseEvent}
				/>
			</box>
			<box
				position="relative"
				flexGrow={1}
				width="100%"
				flexDirection="row"
				overflow="hidden"
				onMouseDown={handleHostMouseDown}
			>
				<box
					flexGrow={primaryVisible() && !layout() && !wideCollapsed() ? 1 : 0}
					flexShrink={0}
					width={
						primaryVisible()
							? wideCollapsed()
								? Math.max(1, hostWidth() - COLLAPSED_HANDLE_WIDTH)
								: (layout()?.primaryColumns ?? "100%")
							: 0
					}
					height="100%"
					overflow="hidden"
					onMouseDown={handleHostMouseDown}
				>
					{props.children}
				</box>
				{/* Keep this container in the flex flow even while hidden. OpenTUI can
				 * retain stale descendant coordinates when a mounted container changes
				 * from off-screen absolute positioning back to relative positioning. */}
				<box
					position="relative"
					visible={secondaryVisible()}
					flexShrink={0}
					border={layout() && secondaryVisible() ? ["left"] : false}
					borderColor={theme.borderDefault}
					width={
						secondaryVisible()
							? (layout()?.secondaryColumns ?? Math.max(1, hostWidth()))
							: 0
					}
					height="100%"
					overflow="hidden"
					flexDirection="column"
					onMouseDown={handleHostMouseDown}
				>
					<box
						position="relative"
						width="100%"
						height={narrowTabbed() ? 0 : 2}
						flexShrink={0}
						overflow="hidden"
					>
						<WorkspaceTabStrip
							mode="wide"
							visible={() => !narrowTabbed()}
							width={() => layout()?.secondaryColumns ?? hostWidth()}
							tabs={props.tabs}
							activeTabId={props.activeTabId}
							selectedSurface={props.selectedSurface}
							onSelectTranscript={props.onSelectTranscript}
							onSelectTab={props.onSelectTab}
							onCloseTab={props.onCloseTab}
							onCollapse={props.onCollapseDrawer}
							onOpenOverflow={props.onOpenOverflow}
							onMouseHandlerReady={(handler) => {
								wideTabMouseHandler = handler;
							}}
							shouldHandleMouseEvent={shouldHandleTabMouseEvent}
						/>
					</box>
					<box position="relative" flexGrow={1} width="100%" overflow="hidden">
						<SecondarySurface />
					</box>
				</box>
				<box
					position="relative"
					visible={wideCollapsed()}
					width={wideCollapsed() ? COLLAPSED_HANDLE_WIDTH : 0}
					height="100%"
					flexGrow={wideCollapsed() ? 1 : 0}
					flexShrink={0}
					overflow="hidden"
					ref={(value) => {
						handleRef = value as HandleRef;
					}}
					alignItems="center"
					backgroundColor={
						handleHovered() || handlePressed() ? theme.bgMuted : theme.bgSurface
					}
					onMouseOver={() => {
						setHandleHovered(true);
						renderer.setMousePointer("pointer");
					}}
					onMouseOut={() => {
						setHandleHovered(false);
						setHandlePressed(false);
						renderer.setMousePointer("default");
					}}
					onMouseDown={(event) => {
						if (
							event.button !== PRIMARY_MOUSE_BUTTON ||
							!shouldHandleTabMouseEvent(event)
						) {
							return;
						}
						consumeMouse(event);
						setHandlePressed(true);
					}}
					onMouseUp={(event) => {
						if (event.button !== PRIMARY_MOUSE_BUTTON) return;
						consumeMouse(event);
						const shouldExpand = handlePressed();
						setHandlePressed(false);
						if (shouldExpand) props.onExpandDrawer();
					}}
				>
					<box
						position="absolute"
						left={0}
						top="50%"
						width="100%"
						height={1}
						alignItems="flex-start"
					>
						<text fg={handleHovered() ? theme.textPrimary : theme.textMuted}>
							{ANGLE_LEFT}
						</text>
					</box>
				</box>
				<box
					position="absolute"
					left={layout() && !wideCollapsed() ? layout()?.primaryColumns : -1000}
					top={0}
					width={1}
					height="100%"
					zIndex={1}
					backgroundColor={
						dividerHovered() || dragging() ? theme.borderAccent : undefined
					}
					onMouseOver={() => {
						setDividerHovered(true);
						renderer.setMousePointer("move");
					}}
					onMouseOut={() => {
						setDividerHovered(false);
						if (!dragging()) renderer.setMousePointer("default");
					}}
					onMouseDown={(event) => {
						if (
							event.button !== PRIMARY_MOUSE_BUTTON ||
							!layout() ||
							wideCollapsed()
						) {
							return;
						}
						consumeMouse(event);
						props.onDividerMouseDown?.();
						setDragRatio(props.preferredPaneRatio());
						setDragging(true);
						renderer.setMousePointer("move");
					}}
					onMouseDrag={updateRatio}
					onMouseDragEnd={finishDrag}
					onMouseUp={finishDrag}
				/>
			</box>
		</box>
	);
}
