import type { MouseEvent } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import type { JSX } from "solid-js";
import { createMemo, createSignal, onCleanup, Show } from "solid-js";
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
			flexDirection="row"
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
				flexGrow={layout() ? 0 : 1}
				flexShrink={layout() ? 0 : 1}
				width={layout()?.primaryColumns}
				height="100%"
			>
				{props.children}
			</box>
			<Show when={layout()}>
				{(current) => (
					<>
						<box
							flexShrink={0}
							width={current().secondaryColumns}
							height="100%"
						>
							{props.secondary}
						</box>
						<box
							position="absolute"
							left={current().primaryColumns}
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
								if (event.button !== PRIMARY_MOUSE_BUTTON) return;
								consumeMouse(event);
								props.onDividerMouseDown?.();
								setDragging(true);
								renderer.setMousePointer("move");
							}}
							onMouseDrag={(event) => {
								updateRatio(event);
							}}
							onMouseDragEnd={finishDrag}
							onMouseUp={finishDrag}
						/>
					</>
				)}
			</Show>
		</box>
	);
}
