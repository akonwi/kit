import type {
	BoxRenderable,
	MouseEvent,
	ScrollBoxRenderable,
} from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { createMemo, createSignal, For } from "solid-js";
import { scrollbarStyle, theme } from "../theme";

const BASH_OUTPUT_MAX_HEIGHT = 14;
const OUTPUT_TAB_WIDTH = 2;
const graphemeSegmenter = new Intl.Segmenter(undefined, {
	granularity: "grapheme",
});

type ScrollableTextRenderable = {
	scrollX: number;
	scrollY: number;
};

function BashOutputLine(props: {
	children: string;
	onMouseScroll: (event: MouseEvent) => void;
}) {
	let textRef: ScrollableTextRenderable | undefined;
	return (
		<text
			ref={(value) => {
				textRef = value as ScrollableTextRenderable;
			}}
			fg={theme.textPrimary}
			bg={theme.bgSurface}
			width="100%"
			height={1}
			wrapMode="none"
			onMouseScroll={(event) => {
				props.onMouseScroll(event);
				// Keep wheel input on the surrounding output scrollbox rather than
				// allowing a wrapped text line to retain its own scroll offset.
				queueMicrotask(() => {
					if (!textRef) return;
					textRef.scrollX = 0;
					textRef.scrollY = 0;
				});
			}}
		>
			{props.children}
		</text>
	);
}

function wrapOutputLine(line: string, maxWidth: number): string[] {
	if (!line) return [""];
	const rows: string[] = [];
	let row = "";
	let rowWidth = 0;
	const expanded = line.replaceAll("\t", " ".repeat(OUTPUT_TAB_WIDTH));
	for (const { segment } of graphemeSegmenter.segment(expanded)) {
		const width = Bun.stringWidth(segment);
		if (row && rowWidth + width > maxWidth) {
			rows.push(row);
			row = "";
			rowWidth = 0;
		}
		row += segment;
		rowWidth += width;
	}
	if (row || rows.length === 0) rows.push(row);
	return rows;
}

export function BashOutputWell(props: {
	lines: string[];
	stickyBottom?: boolean;
}) {
	const renderer = useRenderer();
	let wellRef: BoxRenderable | undefined;
	let scrollRef: ScrollBoxRenderable | undefined;
	const [contentColumns, setContentColumns] = createSignal(
		Math.max(1, renderer.width - 3),
	);
	const wrappedLines = createMemo(() =>
		props.lines.flatMap((line) => wrapOutputLine(line, contentColumns())),
	);
	const viewportHeight = () =>
		Math.min(BASH_OUTPUT_MAX_HEIGHT, Math.max(1, wrappedLines().length));
	const overflowing = () => wrappedLines().length > viewportHeight();

	const scrollDelta = (event: MouseEvent) =>
		event.scroll?.direction === "up"
			? -1
			: event.scroll?.direction === "down"
				? 1
				: 0;

	const canScroll = (delta: number) => {
		if (!scrollRef) return false;
		const maxScroll = Math.max(
			0,
			scrollRef.scrollHeight - scrollRef.viewport.height,
		);
		return (
			(delta < 0 && scrollRef.scrollTop > 0) ||
			(delta > 0 && scrollRef.scrollTop < maxScroll)
		);
	};

	const handleLineMouseScroll = (event: MouseEvent) => {
		const delta = scrollDelta(event);
		if (!scrollRef || !canScroll(delta)) return;
		// A line listener runs before its parent scrollbox, so consume and apply
		// the movement here to keep the outer activity panel stationary.
		event.preventDefault();
		event.stopPropagation();
		scrollRef.scrollBy({ x: 0, y: delta });
	};

	const handleViewportMouseScroll = (event: MouseEvent) => {
		// ScrollBox still performs its own movement after this listener. Stop
		// propagation only while it has room; at an edge the activity panel takes over.
		if (canScroll(scrollDelta(event))) event.stopPropagation();
	};

	return (
		<box
			ref={(value) => {
				wellRef = value;
				setContentColumns(Math.max(1, wellRef.width - 3));
			}}
			onSizeChange={() => {
				if (wellRef) setContentColumns(Math.max(1, wellRef.width - 3));
			}}
			backgroundColor={theme.bgSurface}
			flexDirection="column"
			width="100%"
		>
			<scrollbox
				ref={(value) => {
					scrollRef = value;
				}}
				height={viewportHeight()}
				scrollY
				stickyScroll={props.stickyBottom}
				stickyStart={props.stickyBottom ? "bottom" : undefined}
				paddingX={1}
				rootOptions={{ backgroundColor: theme.bgSurface }}
				wrapperOptions={{ backgroundColor: theme.bgSurface }}
				viewportOptions={{ backgroundColor: theme.bgSurface }}
				contentOptions={{
					backgroundColor: theme.bgSurface,
					flexDirection: "column",
					gap: 0,
					width: "100%",
				}}
				style={scrollbarStyle()}
				onMouseScroll={handleViewportMouseScroll}
			>
				<For each={wrappedLines()}>
					{(line) => (
						<BashOutputLine onMouseScroll={handleLineMouseScroll}>
							{line}
						</BashOutputLine>
					)}
				</For>
			</scrollbox>
			<box
				visible={overflowing()}
				height={1}
				paddingX={1}
				justifyContent="flex-end"
				backgroundColor={theme.bgSurface}
			>
				<text fg={theme.metaText} bg={theme.bgSurface}>
					{overflowing()
						? `${props.lines.length} ${props.lines.length === 1 ? "line" : "lines"}`
						: " "}
				</text>
			</box>
		</box>
	);
}
