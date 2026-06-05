import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import type { DiffLineAnnotation } from "@pierre/diffs";
import { type Accessor, createMemo, createSelector, For, Show } from "solid-js";
import type { ReviewDiffView } from "../../settings";
import { MessageComposer, type TextareaRef } from "../../shell/MessageComposer";
import { syntaxStyle, theme } from "../../shell/theme";
import type { ReviewHunk } from "./model";
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
	onLineMouseDown?: (
		line: ReviewDiffCommentableLine,
		event: TuiMouseEvent,
	) => void;
	/** Columns available for line content; enables wrap-aware row heights. */
	contentColumns?: number;
};

function wrappedRowsFor(text: string, contentColumns?: number): number {
	if (!contentColumns || contentColumns <= 0) return 1;
	return Math.max(1, estimateWrappedRows(text, contentColumns));
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
	height: number = 1,
) {
	let contentRef: ScrollableTextRenderable | undefined;
	const bg = () => backgroundColor?.() ?? contentBackgroundForKind(kind);
	if (filetype && kind !== "metadata" && kind !== "empty") {
		return (
			<code
				ref={(value) => {
					contentRef = value as ScrollableTextRenderable | undefined;
				}}
				content={text}
				filetype={filetype}
				syntaxStyle={syntaxStyle()}
				bg={bg()}
				conceal={false}
				wrapMode="word"
				onMouseScroll={() => resetLineTextScroll(contentRef)}
				flexGrow={1}
				height={height}
			/>
		);
	}
	return (
		<text
			ref={(value) => {
				contentRef = value as ScrollableTextRenderable | undefined;
			}}
			fg={textColorForKind(kind)}
			bg={bg()}
			onMouseScroll={() => resetLineTextScroll(contentRef)}
			wrapMode="word"
			flexGrow={1}
			height={height}
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

function renderUnifiedRow(
	row: ReviewDiffUnifiedRow,
	lineNumberWidth: number,
	filetype: string | undefined,
	hunk?: ReviewHunk,
	isActiveLine?: (key: string) => boolean,
	onLineMouseDown?: ReviewDiffBlockProps["onLineMouseDown"],
	rowHeight: number = 1,
) {
	const activeKey = () =>
		row.lineIndex == null ? null : `line:${row.lineIndex}`;
	const active = () => {
		const key = activeKey();
		return key != null && (isActiveLine?.(key) ?? false);
	};
	const bg = () =>
		active() ? cursorBackgroundForKind(row.kind) : backgroundForKind(row.kind);
	const commentableLine = () =>
		hunk && row.lineIndex != null
			? getReviewDiffCommentableLine(hunk, row.lineIndex)
			: null;
	const lineId = () => {
		const line = commentableLine();
		return hunk && line
			? getReviewDiffActiveLineId(hunk.id, line.index)
			: undefined;
	};
	const handleMouseDown = (event: TuiMouseEvent) => {
		const line = commentableLine();
		if (!line) return;
		onLineMouseDown?.(line, event);
	};
	return (
		<box
			id={lineId()}
			flexDirection="row"
			alignItems="flex-start"
			backgroundColor={bg()}
			height={rowHeight}
			flexShrink={0}
			onMouseDown={handleMouseDown}
		>
			<text
				fg={theme.textMuted}
				bg={active() ? theme.diffCursorGutterBg : bg()}
				flexShrink={0}
				height={1}
			>
				{formatLineNumber(row.deletionLineNumber, lineNumberWidth)}
			</text>
			<text
				fg={theme.textMuted}
				bg={active() ? theme.diffCursorGutterBg : bg()}
				flexShrink={0}
				height={1}
			>
				{" "}
				{formatLineNumber(row.additionLineNumber, lineNumberWidth)}
			</text>
			<text fg={signColorForKind(row.kind)} bg={bg()} flexShrink={0} height={1}>
				{row.sign}{" "}
			</text>
			{renderContentText(row.text, row.kind, filetype, bg, rowHeight)}
		</box>
	);
}

function renderSplitCell(
	cell: ReviewDiffCell,
	lineNumberWidth: number,
	filetype: string | undefined,
	hunk: ReviewHunk,
	isActiveLine?: (key: string) => boolean,
	onLineMouseDown?: ReviewDiffBlockProps["onLineMouseDown"],
	cellHeight: number = 1,
) {
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
			id={lineId()}
			width="50%"
			flexDirection="row"
			alignItems="flex-start"
			backgroundColor={bg()}
			height={cellHeight}
			flexShrink={0}
			onMouseDown={handleMouseDown}
		>
			<text
				fg={theme.textMuted}
				bg={active() ? theme.diffCursorGutterBg : bg()}
				flexShrink={0}
				height={1}
			>
				{formatLineNumber(cell.lineNumber, lineNumberWidth)}
			</text>
			<text
				fg={signColorForKind(cell.kind)}
				bg={bg()}
				flexShrink={0}
				height={1}
			>
				{cell.sign}{" "}
			</text>
			{renderContentText(cell.text, cell.kind, filetype, bg, cellHeight)}
		</box>
	);
}

function rawPatchRows(rawPatch: string): ReviewDiffUnifiedRow[] {
	return rawPatch
		.replace(/\r\n/g, "\n")
		.split("\n")
		.filter((line) => line.length > 0)
		.map((line, index) => {
			const kind =
				line.startsWith("@@") ||
				line.startsWith("diff --git") ||
				line.startsWith("index ") ||
				line.startsWith("--- ") ||
				line.startsWith("+++ ")
					? "metadata"
					: line.startsWith("+")
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
				<box flexDirection="column" gap={0}>
					<For each={rawPatchRows(props.rawPatch ?? "")}>
						{(row) => renderUnifiedRow(row, 1, props.filetype)}
					</For>
				</box>
			}
		>
			{(hunk) => {
				const currentHunk = () => hunk();
				const lineNumberWidth = () => getLineNumberWidth(currentHunk());
				return (
					<Show
						when={props.view === "split"}
						fallback={
							<box flexDirection="column" gap={0}>
								<For each={buildReviewDiffUnifiedRows(currentHunk())}>
									{(row) => (
										<box
											flexDirection="column"
											gap={0}
											width="100%"
											flexShrink={0}
										>
											{renderUnifiedRow(
												row,
												lineNumberWidth(),
												props.filetype,
												currentHunk(),
												isActiveUnifiedLine,
												props.onLineMouseDown,
												wrappedRowsFor(row.text, props.contentColumns),
											)}
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
						<box flexDirection="column" gap={0}>
							<For each={buildReviewDiffSplitRows(currentHunk())}>
								{(row) => {
									const rowHeight = Math.max(
										wrappedRowsFor(row.deletion.text, props.contentColumns),
										wrappedRowsFor(row.addition.text, props.contentColumns),
									);
									return (
										<box
											flexDirection="column"
											gap={0}
											width="100%"
											flexShrink={0}
										>
											<box
												flexDirection="row"
												alignItems="flex-start"
												width="100%"
												flexShrink={0}
											>
												{renderSplitCell(
													row.deletion,
													lineNumberWidth(),
													props.filetype,
													currentHunk(),
													isActiveSplitLine,
													props.onLineMouseDown,
													rowHeight,
												)}
												{renderSplitCell(
													row.addition,
													lineNumberWidth(),
													props.filetype,
													currentHunk(),
													isActiveSplitLine,
													props.onLineMouseDown,
													rowHeight,
												)}
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
