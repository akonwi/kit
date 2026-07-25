import type { MouseEvent } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import type { JSX } from "solid-js";
import { createMemo, createSignal, onCleanup } from "solid-js";
import { theme } from "./theme";
import {
	preferredPaneRatioFromDivider,
	resolveWorkspacePaneLayout,
} from "./workspace-layout";

export type WorkspacePaneHostProps = {
	children: JSX.Element;
	secondary: JSX.Element;
	secondaryOpen: boolean;
	initialWidth: number;
	preferredPaneRatio: number;
	minPrimaryColumns: number;
	minSecondaryColumns: number;
	onPreferredPaneRatioChange: (ratio: number) => void;
	onPreferredPaneRatioCommit: (ratio: number) => void;
	onDividerMouseDown?: () => void;
	narrowTabs?: {
		selected: () => "transcript" | "review";
		onSelect: (tab: "transcript" | "review") => void;
	};
};

const PRIMARY_MOUSE_BUTTON = 0;

type HostRef = {
	width: number;
	screenX: number;
};

export function WorkspacePaneHost(props: WorkspacePaneHostProps) {
	const renderer = useRenderer();
	let hostRef: HostRef | undefined;
	const [hostWidth, setHostWidth] = createSignal(props.initialWidth);
	const [dividerHovered, setDividerHovered] = createSignal(false);
	const [dragging, setDragging] = createSignal(false);
	const layout = createMemo(() => {
		if (!props.secondaryOpen) return null;
		return resolveWorkspacePaneLayout({
			availableColumns: hostWidth(),
			preferredPaneRatio: props.preferredPaneRatio,
			minPrimaryColumns: props.minPrimaryColumns,
			minSecondaryColumns: props.minSecondaryColumns,
		});
	});
	const narrowTabbed = () =>
		props.secondaryOpen && layout() === null && props.narrowTabs !== undefined;
	const primaryVisible = () =>
		!narrowTabbed() || props.narrowTabs?.selected() === "transcript";
	const secondaryVisible = () =>
		layout() !== null ||
		(narrowTabbed() && props.narrowTabs?.selected() === "review");

	function selectNarrowTab(
		tab: "transcript" | "review",
		event: MouseEvent,
	): void {
		if (event.button !== PRIMARY_MOUSE_BUTTON) return;
		consumeMouse(event);
		props.narrowTabs?.onSelect(tab);
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
			minSecondaryColumns: props.minSecondaryColumns,
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

	onCleanup(() => {
		if (dividerHovered() || dragging()) renderer.setMousePointer("default");
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
				position="absolute"
				top={0}
				left={narrowTabbed() ? 0 : -1000}
				width="100%"
				height={2}
				zIndex={2}
				overflow="hidden"
				paddingX={1}
				flexDirection="row"
				gap={2}
				border={["bottom"]}
				borderColor={theme.borderDefault}
			>
				<text
					fg={
						props.narrowTabs?.selected() === "transcript"
							? theme.textPrimary
							: theme.textMuted
					}
					onMouseDown={(event) => selectNarrowTab("transcript", event)}
				>
					{narrowTabbed()
						? props.narrowTabs?.selected() === "transcript"
							? "[Transcript]"
							: "Transcript"
						: ""}
				</text>
				<text
					fg={
						props.narrowTabs?.selected() === "review"
							? theme.textPrimary
							: theme.textMuted
					}
					onMouseDown={(event) => selectNarrowTab("review", event)}
				>
					{narrowTabbed()
						? props.narrowTabs?.selected() === "review"
							? "[Code review]"
							: "Code review"
						: ""}
				</text>
			</box>
			<box
				flexGrow={1}
				width="100%"
				marginTop={narrowTabbed() ? 2 : 0}
				flexDirection="row"
				overflow="hidden"
			>
				<box
					flexGrow={primaryVisible() && !layout() ? 1 : 0}
					flexShrink={0}
					width={primaryVisible() ? (layout()?.primaryColumns ?? "100%") : 0}
					height="100%"
					overflow="hidden"
				>
					{props.children}
				</box>
				<box
					flexShrink={0}
					width={
						props.secondaryOpen && secondaryVisible()
							? (layout()?.secondaryColumns ?? "100%")
							: 0
					}
					height="100%"
					overflow="hidden"
				>
					{props.secondary}
				</box>
				<box
					position="absolute"
					left={layout()?.primaryColumns ?? -1000}
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
						if (event.button !== PRIMARY_MOUSE_BUTTON || !layout()) return;
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
