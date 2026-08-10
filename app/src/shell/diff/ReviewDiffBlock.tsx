import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import type { DiffLineAnnotation } from "@pierre/diffs";
import {
	type Accessor,
	createEffect,
	createMemo,
	createSelector,
	createSignal,
	For,
	Show,
} from "solid-js";
import type { ReviewDiffView } from "../../settings";
import { DASHED_VERTICAL, DIAMOND } from "../glyphs";
import { MessageComposer, type TextareaRef } from "../MessageComposer";
import { syntaxStyle, theme } from "../theme";
import {
	buildReviewDiffSplitRows,
	buildReviewDiffUnifiedRows,
	estimateWrappedRows,
	getReviewDiffActiveLineId,
	getReviewDiffAnnotationHeight,
	getReviewDiffCommentableLine,
	getReviewDiffSplitAnnotationsAfterRow,
	getReviewDiffUnifiedAnnotationsAfterRow,
	type ReviewDiffAnnotationMetadata,
	type ReviewDiffCell,
	type ReviewDiffCellKind,
	type ReviewDiffCommentableLine,
	type ReviewDiffSplitAnnotationGroup,
	type ReviewDiffUnifiedRow,
} from "./ReviewDiffModel";
import type { ReviewHunk } from "./types";

export {
	buildReviewDiffSplitRows,
	buildReviewDiffUnifiedRows,
	estimateWrappedRows,
	getReviewDiffActiveLineId,
	getReviewDiffCommentableLine,
	getReviewDiffCommentableLines,
	getReviewDiffLineHeight,
	getReviewDiffLineTop,
	getReviewDiffRangeBounds,
	type ReviewDiffAnnotationMetadata,
	type ReviewDiffCell,
	type ReviewDiffCellKind,
	type ReviewDiffCommentableLine,
	type ReviewDiffLineRange,
	type ReviewDiffSide,
	type ReviewDiffSplitRow,
	type ReviewDiffUnifiedRow,
	type ReviewDiffVisualBounds,
	shouldResetPatchScroll,
} from "./ReviewDiffModel";

export type ReviewDiffBlockProps = {
	view: ReviewDiffView;
	hunk?: ReviewHunk;
	rawPatch?: string;
	filetype?: string;
	annotations?: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[];
	annotationEditor?: {
		onChange: (value: string) => void;
		onSubmit: () => void;
	};
	activeLine?: ReviewDiffCommentableLine;
	lineMarker?: (
		line: ReviewDiffCommentableLine,
	) => "anchor" | "range" | undefined;
	onLineMouseDown?: (
		line: ReviewDiffCommentableLine,
		event: TuiMouseEvent,
	) => void;
	/** Columns available for line content; enables wrap-aware row heights. */
	contentColumns?: number | Accessor<number | undefined>;
	/** Columns overlaid by surrounding chrome, such as a vertical scrollbar. */
	contentRightInset?: number;
	/**
	 * Gutter width override. When rendering multiple blocks for one file
	 * (hunks + expanded unchanged sections), pass a file-wide width so
	 * gutters align vertically instead of sizing to each block's own max
	 * line number.
	 */
	lineNumberWidth?: number;
	/**
	 * Whether to render the line-number gutter. Defaults to true. Callers
	 * displaying diffs without reliable absolute file positions (e.g. the
	 * synthetic hunks built for the `edit` tool) should pass `false`.
	 */
	showLineNumbers?: boolean;
};

function wrappedRowsFor(text: string, contentColumns?: number): number {
	if (!contentColumns || contentColumns <= 0) return 1;
	return Math.max(1, estimateWrappedRows(text, contentColumns));
}

function LineMarker(props: {
	backgroundColor: Accessor<string>;
	height: Accessor<number>;
	marker: Accessor<"anchor" | "range" | undefined>;
}) {
	const content = () => {
		const marker = props.marker();
		if (!marker) return " ";
		const glyph = marker === "anchor" ? DIAMOND : DASHED_VERTICAL;
		return Array.from({ length: props.height() }, () => glyph).join("\n");
	};
	return (
		<text
			fg={props.marker() === "range" ? theme.borderAccent : theme.borderFocused}
			bg={props.backgroundColor()}
			width={1}
			height={props.height()}
			flexShrink={0}
		>
			{content()}
		</text>
	);
}

function getLineNumberWidth(hunk: ReviewHunk): number {
	const maxLineNumber = Math.max(
		0,
		...hunk.lines.flatMap((line) => [
			line.deletionLineNumber ?? 0,
			line.additionLineNumber ?? 0,
		]),
	);
	return Math.max(1, String(maxLineNumber).length);
}

function formatLineNumber(
	lineNumber: number | undefined,
	width: number,
): string {
	return lineNumber == null
		? " ".repeat(width)
		: String(lineNumber).padStart(width);
}

function cursorBackgroundForKind(kind: ReviewDiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.diffCursorAddedBg;
		case "delete":
			return theme.diffCursorRemovedBg;
		default:
			return theme.diffCursorBg;
	}
}

function backgroundForKind(kind: ReviewDiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.diffAddedBg;
		case "delete":
			return theme.diffRemovedBg;
		case "metadata":
			return theme.bgMuted;
		default:
			return theme.bgSurface;
	}
}

function contentBackgroundForKind(kind: ReviewDiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.diffAddedContentBg;
		case "delete":
			return theme.diffRemovedContentBg;
		default:
			return backgroundForKind(kind);
	}
}

function signColorForKind(kind: ReviewDiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.toolText;
		case "delete":
			return theme.errorText;
		case "metadata":
			return theme.metaText;
		default:
			return theme.textMuted;
	}
}

function textColorForKind(kind: ReviewDiffCellKind): string {
	if (kind === "metadata") return theme.metaText;
	if (kind === "empty") return theme.textPlaceholder;
	return theme.textPrimary;
}

type ScrollableTextRenderable = {
	scrollX: number;
	scrollY: number;
	width: number;
};

function resetLineTextScroll(ref: ScrollableTextRenderable | undefined) {
	// OpenTUI dispatches mouse listeners before a text renderable's own
	// scroll handler, and stopPropagation would block the parent scrollbox.
	// Defer the reset so the diff scrollbox can scroll while the line-local
	// text buffer offset is restored after its internal handler runs.
	queueMicrotask(() => {
		if (!ref) return;
		ref.scrollX = 0;
		ref.scrollY = 0;
	});
}

function renderContentText(
	text: string,
	kind: ReviewDiffCellKind,
	filetype: string | undefined,
	backgroundColor?: Accessor<string>,
	height?: number | Accessor<number | undefined>,
	onRenderable?: (renderable: ScrollableTextRenderable) => void,
	width?: Accessor<number | undefined>,
) {
	let contentRef: ScrollableTextRenderable | undefined;
	const bg = () => backgroundColor?.() ?? contentBackgroundForKind(kind);
	const resolvedHeight = () =>
		typeof height === "function" ? height() : height;
	if (filetype && kind !== "metadata" && kind !== "empty") {
		return (
			<code
				ref={(value) => {
					contentRef = value as ScrollableTextRenderable | undefined;
					if (contentRef) onRenderable?.(contentRef);
				}}
				content={text}
				filetype={filetype}
				syntaxStyle={syntaxStyle()}
				bg={bg()}
				conceal={false}
				wrapMode="word"
				onMouseScroll={() => resetLineTextScroll(contentRef)}
				flexBasis={width?.() == null ? 0 : undefined}
				flexGrow={width?.() == null ? 1 : 0}
				flexShrink={1}
				minWidth={1}
				height={resolvedHeight()}
				width={width?.()}
			/>
		);
	}
	return (
		<text
			ref={(value) => {
				contentRef = value as ScrollableTextRenderable | undefined;
				if (contentRef) onRenderable?.(contentRef);
			}}
			fg={textColorForKind(kind)}
			bg={bg()}
			onMouseScroll={() => resetLineTextScroll(contentRef)}
			wrapMode="word"
			flexBasis={width?.() == null ? 0 : undefined}
			flexGrow={width?.() == null ? 1 : 0}
			flexShrink={1}
			minWidth={1}
			height={resolvedHeight()}
			width={width?.()}
		>
			{text}
		</text>
	);
}

function renderAnnotationContent(
	annotation: DiffLineAnnotation<ReviewDiffAnnotationMetadata>,
	editor?: ReviewDiffBlockProps["annotationEditor"],
) {
	if (annotation.metadata.editing && editor) {
		let textareaRef: TextareaRef | undefined;
		return (
			<MessageComposer
				ref={(value) => {
					textareaRef = value;
				}}
				initialValue={annotation.metadata.comment}
				placeholder="Type your review note..."
				backgroundColor={theme.bgTransparent}
				focusedBackgroundColor={theme.bgTransparent}
				keyBindings={[
					{ name: "return", action: "submit" },
					{ name: "return", shift: true, action: "newline" },
				]}
				onContentChange={() => editor.onChange(textareaRef?.plainText ?? "")}
				onSubmit={editor.onSubmit}
			/>
		);
	}
	return (
		<box
			border
			borderColor={theme.borderDefault}
			backgroundColor={theme.bgSurface}
			paddingX={1}
			width="100%"
			height={getReviewDiffAnnotationHeight(annotation)}
			flexShrink={0}
		>
			<text fg={theme.textPrimary} bg={theme.bgSurface}>
				{annotation.metadata.comment}
			</text>
		</box>
	);
}

function renderUnifiedAnnotationRow(
	annotation: DiffLineAnnotation<ReviewDiffAnnotationMetadata>,
	editor?: ReviewDiffBlockProps["annotationEditor"],
) {
	return (
		<box
			height={getReviewDiffAnnotationHeight(annotation)}
			flexShrink={0}
			width="100%"
		>
			{renderAnnotationContent(annotation, editor)}
		</box>
	);
}

function renderSplitAnnotationCell(
	annotation: DiffLineAnnotation<ReviewDiffAnnotationMetadata> | undefined,
	rowHeight: number,
	editor?: ReviewDiffBlockProps["annotationEditor"],
) {
	return (
		<box width="50%" height={rowHeight} flexShrink={0}>
			<Show when={annotation}>
				{(value) => renderAnnotationContent(value(), editor)}
			</Show>
		</box>
	);
}

function renderSplitAnnotationRows(
	group: ReviewDiffSplitAnnotationGroup,
	editor?: ReviewDiffBlockProps["annotationEditor"],
) {
	const rows = Array.from(
		{ length: Math.max(group.deletions.length, group.additions.length) },
		(_, index) => {
			const deletion = group.deletions[index];
			const addition = group.additions[index];
			return {
				deletion,
				addition,
				height: Math.max(
					getReviewDiffAnnotationHeight(deletion),
					getReviewDiffAnnotationHeight(addition),
				),
			};
		},
	);
	return (
		<For each={rows}>
			{(row) => (
				<box
					flexDirection="row"
					width="100%"
					height={row.height}
					flexShrink={0}
				>
					{renderSplitAnnotationCell(row.deletion, row.height, editor)}
					{renderSplitAnnotationCell(row.addition, row.height, editor)}
				</box>
			)}
		</For>
	);
}

type UnifiedRowProps = {
	row: ReviewDiffUnifiedRow;
	lineNumberWidth: number;
	filetype: string | undefined;
	contentColumns?: Accessor<number | undefined>;
	contentRightInset?: number;
	hunk?: ReviewHunk;
	isActiveLine?: (key: string) => boolean;
	lineMarker?: ReviewDiffBlockProps["lineMarker"];
	onLineMouseDown?: ReviewDiffBlockProps["onLineMouseDown"];
	showLineNumbers?: boolean;
};

function UnifiedRow(props: UnifiedRowProps) {
	let rowRef: { width: number; height: number } | undefined;
	let contentRef: ScrollableTextRenderable | undefined;
	let measuredColumns: number | undefined;
	const gutterColumns = () =>
		props.showLineNumbers !== false ? 2 * props.lineNumberWidth + 3 : 2;
	const activeKey = () =>
		props.row.lineIndex == null ? null : `line:${props.row.lineIndex}`;
	const active = () => {
		const key = activeKey();
		return key != null && (props.isActiveLine?.(key) ?? false);
	};
	const bg = () =>
		active()
			? cursorBackgroundForKind(props.row.kind)
			: backgroundForKind(props.row.kind);
	const rowHeight = () =>
		wrappedRowsFor(props.row.text, props.contentColumns?.());
	const commentableLine = () =>
		props.hunk && props.row.lineIndex != null
			? getReviewDiffCommentableLine(props.hunk, props.row.lineIndex)
			: null;
	const lineId = () => {
		const line = commentableLine();
		return props.hunk && line
			? getReviewDiffActiveLineId(props.hunk.id, line.index)
			: undefined;
	};
	const marker = () => {
		const line = commentableLine();
		return line ? props.lineMarker?.(line) : undefined;
	};
	const handleMouseDown = (event: TuiMouseEvent) => {
		const line = commentableLine();
		if (!line) return;
		props.onLineMouseDown?.(line, event);
	};
	const updateLayout = () => {
		const ref = rowRef;
		if (!ref) return;
		const availableColumns = Math.max(
			1,
			ref.width - gutterColumns() - (props.contentRightInset ?? 0),
		);
		const contentColumns = Math.min(
			availableColumns,
			props.contentColumns?.() ?? availableColumns,
		);
		if (contentRef && contentRef.width !== contentColumns) {
			contentRef.width = contentColumns;
		}
		if (measuredColumns === contentColumns) return;
		measuredColumns = contentColumns;
		const height = wrappedRowsFor(props.row.text, contentColumns);
		if (ref.height !== height) ref.height = height;
	};
	createEffect(() => {
		props.contentColumns?.();
		props.contentRightInset;
		updateLayout();
	});
	return (
		<box
			ref={(value) => {
				rowRef = value as typeof rowRef;
			}}
			id={lineId()}
			flexDirection="row"
			alignItems="flex-start"
			backgroundColor={bg()}
			height={rowHeight()}
			flexShrink={0}
			width="100%"
			onSizeChange={updateLayout}
			onMouseDown={handleMouseDown}
		>
			<Show when={props.showLineNumbers !== false}>
				<text
					fg={theme.textMuted}
					bg={active() ? theme.diffCursorGutterBg : bg()}
					flexShrink={0}
					height={1}
				>
					{formatLineNumber(
						props.row.deletionLineNumber,
						props.lineNumberWidth,
					)}
				</text>
				<text
					fg={theme.textMuted}
					bg={active() ? theme.diffCursorGutterBg : bg()}
					flexShrink={0}
					height={1}
				>
					{" "}
					{formatLineNumber(
						props.row.additionLineNumber,
						props.lineNumberWidth,
					)}
				</text>
			</Show>
			<text
				fg={signColorForKind(props.row.kind)}
				bg={bg()}
				flexShrink={0}
				height={1}
			>
				{props.row.sign}
			</text>
			<LineMarker backgroundColor={bg} height={rowHeight} marker={marker} />
			{renderContentText(
				props.row.text,
				props.row.kind,
				props.filetype,
				bg,
				undefined,
				(renderable) => {
					contentRef = renderable;
					const contentColumns = props.contentColumns?.();
					if (contentColumns != null) {
						renderable.width = contentColumns;
					}
				},
				props.contentColumns,
			)}
		</box>
	);
}

type SplitCellLayout = {
	box?: { height: number };
	content?: ScrollableTextRenderable;
};

type RenderSplitCellOptions = {
	cell: ReviewDiffCell;
	lineNumberWidth: number;
	filetype: string | undefined;
	hunk: ReviewHunk;
	isActiveLine?: (key: string) => boolean;
	lineMarker?: ReviewDiffBlockProps["lineMarker"];
	onLineMouseDown?: ReviewDiffBlockProps["onLineMouseDown"];
	cellHeight: Accessor<number>;
	contentColumns: Accessor<number | undefined>;
	layout?: SplitCellLayout;
	onLayoutReady?: () => void;
	showLineNumbers?: boolean;
};

function renderSplitCell(opts: RenderSplitCellOptions) {
	const {
		cell,
		lineNumberWidth,
		filetype,
		hunk,
		isActiveLine,
		lineMarker,
		onLineMouseDown,
		cellHeight,
		contentColumns,
		layout,
		onLayoutReady,
		showLineNumbers = true,
	} = opts;
	const commentableLine = () =>
		cell.lineIndex != null
			? getReviewDiffCommentableLine(hunk, cell.lineIndex)
			: null;
	const active = () => {
		const line = commentableLine();
		return (
			line != null &&
			(isActiveLine?.(`line:${line.index}:${line.side}`) ?? false)
		);
	};
	const lineId = () => {
		const line = commentableLine();
		return line ? getReviewDiffActiveLineId(hunk.id, line.index) : undefined;
	};
	const marker = () => {
		const line = commentableLine();
		return line ? lineMarker?.(line) : undefined;
	};
	const bg = () =>
		active()
			? cursorBackgroundForKind(cell.kind)
			: backgroundForKind(cell.kind);
	const handleMouseDown = (event: TuiMouseEvent) => {
		const line = commentableLine();
		if (!line) return;
		onLineMouseDown?.(line, event);
	};
	return (
		<box
			ref={(value) => {
				if (layout) layout.box = value as SplitCellLayout["box"];
				onLayoutReady?.();
			}}
			id={lineId()}
			width="50%"
			flexDirection="row"
			alignItems="flex-start"
			backgroundColor={bg()}
			height={cellHeight()}
			flexShrink={0}
			onMouseDown={handleMouseDown}
		>
			<Show when={showLineNumbers}>
				<text
					fg={theme.textMuted}
					bg={active() ? theme.diffCursorGutterBg : bg()}
					flexShrink={0}
					height={1}
				>
					{formatLineNumber(cell.lineNumber, lineNumberWidth)}
				</text>
			</Show>
			<text
				fg={signColorForKind(cell.kind)}
				bg={bg()}
				flexShrink={0}
				height={1}
			>
				{cell.sign}
			</text>
			<LineMarker backgroundColor={bg} height={cellHeight} marker={marker} />
			{renderContentText(
				cell.text,
				cell.kind,
				filetype,
				bg,
				cellHeight,
				(renderable) => {
					if (layout) layout.content = renderable;
					const width = contentColumns();
					if (width != null) renderable.width = width;
					onLayoutReady?.();
				},
				contentColumns,
			)}
		</box>
	);
}

/**
 * Rows for a raw patch string. Patch metadata lines (`diff --git`,
 * `index`, `---`/`+++`, `@@`) are dropped — the review view presents
 * diffs without patch-format jargon.
 */
function rawPatchRows(rawPatch: string): ReviewDiffUnifiedRow[] {
	return rawPatch
		.replace(/\r\n/g, "\n")
		.split("\n")
		.filter(
			(line) =>
				line.length > 0 &&
				!line.startsWith("@@") &&
				!line.startsWith("diff --git") &&
				!line.startsWith("index ") &&
				!line.startsWith("--- ") &&
				!line.startsWith("+++ "),
		)
		.map((line, index) => {
			const kind = line.startsWith("+")
				? "add"
				: line.startsWith("-")
					? "delete"
					: "context";
			return {
				id: `raw:${index}`,
				kind,
				sign: kind === "add" ? "+" : kind === "delete" ? "-" : " ",
				text:
					kind === "add" || kind === "delete" || line.startsWith(" ")
						? line.slice(1)
						: line,
			};
		});
}

export function ReviewDiffBlock(props: ReviewDiffBlockProps) {
	const annotations = () => props.annotations ?? [];
	const contentColumns = () =>
		typeof props.contentColumns === "function"
			? props.contentColumns()
			: props.contentColumns;
	const activeUnifiedLineKey = createMemo(() =>
		props.activeLine ? `line:${props.activeLine.index}` : null,
	);
	const activeSplitLineKey = createMemo(() =>
		props.activeLine
			? `line:${props.activeLine.index}:${props.activeLine.side}`
			: null,
	);
	const isActiveUnifiedLine = createSelector(activeUnifiedLineKey);
	const isActiveSplitLine = createSelector(activeSplitLineKey);
	return (
		<Show
			when={props.hunk}
			fallback={
				<box flexDirection="column" gap={0} width="100%">
					<For each={rawPatchRows(props.rawPatch ?? "")}>
						{(row) => (
							<UnifiedRow
								row={row}
								lineNumberWidth={1}
								filetype={props.filetype}
								showLineNumbers={props.showLineNumbers !== false}
								contentColumns={contentColumns}
								contentRightInset={props.contentRightInset}
							/>
						)}
					</For>
				</box>
			}
		>
			{(hunk) => {
				const currentHunk = () => hunk();
				const lineNumberWidth = () =>
					props.lineNumberWidth ?? getLineNumberWidth(currentHunk());
				return (
					<Show
						when={props.view === "split"}
						fallback={
							<box flexDirection="column" gap={0} width="100%">
								<For each={buildReviewDiffUnifiedRows(currentHunk())}>
									{(row) => (
										<box
											flexDirection="column"
											gap={0}
											width="100%"
											flexShrink={0}
										>
											<UnifiedRow
												row={row}
												lineNumberWidth={lineNumberWidth()}
												filetype={props.filetype}
												contentColumns={contentColumns}
												contentRightInset={props.contentRightInset}
												hunk={currentHunk()}
												isActiveLine={isActiveUnifiedLine}
												lineMarker={props.lineMarker}
												onLineMouseDown={props.onLineMouseDown}
												showLineNumbers={props.showLineNumbers !== false}
											/>
											<For
												each={getReviewDiffUnifiedAnnotationsAfterRow(
													row,
													currentHunk(),
													annotations(),
												)}
											>
												{(annotation) =>
													renderUnifiedAnnotationRow(
														annotation,
														props.annotationEditor,
													)
												}
											</For>
										</box>
									)}
								</For>
							</box>
						}
					>
						<box flexDirection="column" gap={0} width="100%">
							<For each={buildReviewDiffSplitRows(currentHunk())}>
								{(row) => {
									const [measuredContentColumns, setMeasuredContentColumns] =
										createSignal(contentColumns());
									let splitRowRef: { width: number } | undefined;
									const cellLayouts: [SplitCellLayout, SplitCellLayout] = [
										{},
										{},
									];
									const updateSplitLayout = () => {
										if (!splitRowRef) return;
										const halfWidth = Math.floor(
											Math.max(
												1,
												splitRowRef.width - (props.contentRightInset ?? 0),
											) / 2,
										);
										const gutterColumns =
											props.showLineNumbers === false
												? 2
												: lineNumberWidth() + 2;
										const measured = Math.max(1, halfWidth - gutterColumns);
										setMeasuredContentColumns(measured);
										const height = Math.max(
											wrappedRowsFor(row.deletion.text, measured),
											wrappedRowsFor(row.addition.text, measured),
										);
										for (const layout of cellLayouts) {
											if (layout.box && layout.box.height !== height) {
												layout.box.height = height;
											}
											if (layout.content && layout.content.width !== measured) {
												layout.content.width = measured;
											}
										}
									};
									const rowHeight = () =>
										Math.max(
											wrappedRowsFor(
												row.deletion.text,
												measuredContentColumns(),
											),
											wrappedRowsFor(
												row.addition.text,
												measuredContentColumns(),
											),
										);
									return (
										<box
											flexDirection="column"
											gap={0}
											width="100%"
											flexShrink={0}
										>
											<box
												ref={(value) => {
													splitRowRef = value as typeof splitRowRef;
													updateSplitLayout();
												}}
												flexDirection="row"
												alignItems="flex-start"
												width="100%"
												flexShrink={0}
												onSizeChange={updateSplitLayout}
											>
												{renderSplitCell({
													cell: row.deletion,
													lineNumberWidth: lineNumberWidth(),
													filetype: props.filetype,
													hunk: currentHunk(),
													isActiveLine: isActiveSplitLine,
													lineMarker: props.lineMarker,
													onLineMouseDown: props.onLineMouseDown,
													cellHeight: rowHeight,
													contentColumns: measuredContentColumns,
													layout: cellLayouts[0],
													onLayoutReady: updateSplitLayout,
													showLineNumbers: props.showLineNumbers !== false,
												})}
												{renderSplitCell({
													cell: row.addition,
													lineNumberWidth: lineNumberWidth(),
													filetype: props.filetype,
													hunk: currentHunk(),
													isActiveLine: isActiveSplitLine,
													lineMarker: props.lineMarker,
													onLineMouseDown: props.onLineMouseDown,
													cellHeight: rowHeight,
													contentColumns: measuredContentColumns,
													layout: cellLayouts[1],
													onLayoutReady: updateSplitLayout,
													showLineNumbers: props.showLineNumbers !== false,
												})}
											</box>
											{renderSplitAnnotationRows(
												getReviewDiffSplitAnnotationsAfterRow(
													row,
													annotations(),
												),
												props.annotationEditor,
											)}
										</box>
									);
								}}
							</For>
						</box>
					</Show>
				);
			}}
		</Show>
	);
}
