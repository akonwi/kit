import { MouseEvent } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import type { JSX } from "solid-js";
import { createMemo, createSignal, onCleanup } from "solid-js";
import { CHEVRON_LEFT } from "./glyphs";
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

export function WorkspacePaneHost(props: WorkspacePaneHostProps) {
	const renderer = useRenderer();
	const SecondarySurface = props.secondary;
	let hostRef: HostRef | undefined;
	let narrowTabMouseHandler: WorkspaceTabMouseHandler | null = null;
	let wideTabMouseHandler: WorkspaceTabMouseHandler | null = null;
	// Synthetic events are marked so the tab strip can reject a second dispatch
	// if OpenTUI also resolves the same raw press through its normal hit grid.
	const rawMouseEvents = new WeakSet<MouseEvent>();
	const [hostWidth, setHostWidth] = createSignal(props.initialWidth);
	const [dividerHovered, setDividerHovered] = createSignal(false);
	const [handleHovered, setHandleHovered] = createSignal(false);
	const [handlePressed, setHandlePressed] = createSignal(false);
	const [dragging, setDragging] = createSignal(false);
	const hasTabs = () => props.tabs().length > 0;
	const layout = createMemo(() => {
		if (!hasTabs()) return null;
		return resolveWorkspacePaneLayout({
			availableColumns: hostWidth(),
			preferredPaneRatio: props.preferredPaneRatio(),
			minPrimaryColumns: props.minPrimaryColumns,
			minSecondaryColumns: props.minSecondaryColumns(),
		});
	});
	const narrowTabbed = () => hasTabs() && layout() === null;
	const wideCollapsed = () =>
		hasTabs() && layout() !== null && props.drawerCollapsed();
	const primaryVisible = () =>
		!narrowTabbed() || props.selectedSurface() === "transcript";
	const secondaryVisible = () =>
		(layout() !== null && !wideCollapsed()) ||
		(narrowTabbed() && props.selectedSurface() !== "transcript");

	function handleHostMouseDown(event: MouseEvent): boolean {
		if (
			event.defaultPrevented ||
			event.button !== PRIMARY_MOUSE_BUTTON ||
			!hostRef
		) {
			return false;
		}
		const localY = event.y - hostRef.screenY;
		// Some terminals report the painted top-border row one cell above the
		// renderable's Yoga origin, so include that row in the chrome hit area.
		if (localY < -1 || localY >= 2) return false;
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

	function updateRatio(event: MouseEvent): number {
		consumeMouse(event);
		const ratio = ratioAt(event);
		props.onPreferredPaneRatioChange(ratio);
		return ratio;
	}

	function finishDrag(event: MouseEvent): void {
		consumeMouse(event);
		if (!dragging()) return;
		const ratio = ratioAt(event);
		props.onPreferredPaneRatioChange(ratio);
		props.onPreferredPaneRatioCommit(ratio);
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
				handleHostMouseDown(event);
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
			onSizeChange={() => {
				if (hostRef) setHostWidth(hostRef.width);
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
					shouldHandleMouseEvent={(event) => rawMouseEvents.has(event)}
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
					flexGrow={primaryVisible() && (!layout() || wideCollapsed()) ? 1 : 0}
					flexShrink={0}
					width={
						primaryVisible()
							? wideCollapsed()
								? undefined
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
							shouldHandleMouseEvent={(event) => rawMouseEvents.has(event)}
						/>
					</box>
					<box position="relative" flexGrow={1} width="100%" overflow="hidden">
						<SecondarySurface />
					</box>
				</box>
				<box
					position={wideCollapsed() ? "relative" : "absolute"}
					left={wideCollapsed() ? undefined : -1000}
					width={3}
					height="100%"
					flexShrink={0}
					border={["left"]}
					borderColor={
						handleHovered() ? theme.borderAccent : theme.borderDefault
					}
					alignItems="center"
					flexDirection="column"
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
						if (event.button !== PRIMARY_MOUSE_BUTTON) return;
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
					<text fg={theme.metaText}>{CHEVRON_LEFT}</text>
					<text fg={theme.textMuted}>{props.tabs().length}</text>
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
