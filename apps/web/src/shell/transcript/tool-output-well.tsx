import type {
	BoxRenderable,
	MouseEvent,
	ScrollBoxRenderable,
} from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import {
	children,
	createMemo,
	createSignal,
	For,
	type JSX,
	Show,
} from "solid-js";
import { scrollbarStyle, theme } from "../theme";

const TOOL_OUTPUT_MAX_HEIGHT = 14;
const OUTPUT_TAB_WIDTH = 2;
const graphemeSegmenter = new Intl.Segmenter(undefined, {
	granularity: "grapheme",
});

type ScrollableTextRenderable = {
	scrollX: number;
	scrollY: number;
};

function ToolOutputLine(props: {
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

export function ToolOutputWell(props: {
	lines?: string[];
	contentRows?: number | ((contentColumns: number) => number);
	metadata?: string | (() => string);
	stickyBottom?: boolean;
	measureContent?: boolean;
	renderContent?: (contentColumns: number, contentRows: number) => JSX.Element;
	children?: JSX.Element;
}) {
	const renderer = useRenderer();
	let wellRef: BoxRenderable | undefined;
	let richContentRef: BoxRenderable | undefined;
	let scrollRef: ScrollBoxRenderable | undefined;
	const [contentColumns, setContentColumns] = createSignal(
		Math.max(1, renderer.width - 3),
	);
	const richChildren = children(() => props.children);
	const wrappedLines = createMemo(() =>
		(props.lines ?? []).flatMap((line) =>
			wrapOutputLine(line, contentColumns()),
		),
	);
	const estimatedRichRows = createMemo(() => {
		const rows =
			typeof props.contentRows === "function"
				? props.contentRows(contentColumns())
				: (props.contentRows ?? 1);
		return Math.max(1, rows);
	});
	const [measuredRichRows, setMeasuredRichRows] = createSignal(
		TOOL_OUTPUT_MAX_HEIGHT,
	);
	const syncMeasuredRichRows = () => {
		if (!props.measureContent) return;
		queueMicrotask(() => {
			const height = richContentRef?.height ?? 0;
			if (height > 0) setMeasuredRichRows(height);
		});
	};
	const richContentRows = () =>
		props.measureContent ? measuredRichRows() : estimatedRichRows();
	const contentRows = () =>
		props.lines ? wrappedLines().length : richContentRows();
	const viewportHeight = () =>
		Math.min(TOOL_OUTPUT_MAX_HEIGHT, Math.max(1, contentRows()));
	const overflowing = () => contentRows() > viewportHeight();
	const metadata = () => {
		if (typeof props.metadata === "function") return props.metadata();
		if (props.metadata) return props.metadata;
		const lineCount = props.lines?.length ?? 0;
		return `${lineCount} ${lineCount === 1 ? "line" : "lines"}`;
	};

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
				<Show
					when={props.lines}
					fallback={
						<box
							ref={(value) => {
								richContentRef = value;
								syncMeasuredRichRows();
							}}
							onSizeChange={syncMeasuredRichRows}
							width={contentColumns()}
							height={props.measureContent ? undefined : richContentRows()}
							flexDirection="column"
							flexShrink={0}
							onMouseScroll={handleLineMouseScroll}
						>
							{props.renderContent
								? props.renderContent(contentColumns(), richContentRows())
								: richChildren()}
						</box>
					}
				>
					<For each={wrappedLines()}>
						{(line) => (
							<ToolOutputLine onMouseScroll={handleLineMouseScroll}>
								{line}
							</ToolOutputLine>
						)}
					</For>
				</Show>
			</scrollbox>
			<box
				visible={overflowing()}
				height={1}
				paddingX={1}
				justifyContent="flex-end"
				backgroundColor={theme.bgSurface}
			>
				<text fg={theme.metaText} bg={theme.bgSurface}>
					{overflowing() ? metadata() : " "}
				</text>
			</box>
		</box>
	);
}
