import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import type { DiffLineAnnotation } from "@pierre/diffs";
import { type Accessor, createMemo, createSelector, For, Show } from "solid-js";
import type { ReviewDiffView } from "../../settings";
import { MessageComposer, type TextareaRef } from "../../shell/MessageComposer";
import { syntaxStyle, theme } from "../../shell/theme";
import type { ReviewHunk, ReviewLine } from "./model";

export type ReviewDiffSide = "additions" | "deletions";
type DiffCellKind = "add" | "context" | "delete" | "empty" | "metadata";

type DiffCell = {
	kind: DiffCellKind;
	lineIndex?: number;
	lineNumber?: number;
	sign: string;
	text: string;
};

type SplitRow = {
	id: string;
	deletion: DiffCell;
	addition: DiffCell;
};

type UnifiedRow = {
	id: string;
	lineIndex?: number;
	kind: Exclude<DiffCellKind, "empty">;
	deletionLineNumber?: number;
	additionLineNumber?: number;
	sign: string;
	text: string;
};

export type ReviewDiffCommentableLine = {
	index: number;
	side: ReviewDiffSide;
	lineNumber: number;
	text: string;
	kind: Extract<ReviewLine["kind"], "add" | "delete">;
};

export type ReviewDiffVisualBounds = {
	top: number;
	height: number;
};

export type ReviewDiffAnnotationMetadata = {
	key: string;
	comment: string;
	side: ReviewDiffSide;
	startLine: number;
	endLine: number;
	editing?: boolean;
};

export type ReviewDiffLineRange = {
	side: ReviewDiffSide;
	startLine: number;
	endLine: number;
};

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
};

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

function cellForLine(
	line: ReviewLine,
	lineIndex: number,
	side: ReviewDiffSide,
): DiffCell {
	if (line.kind === "context") {
		return {
			kind: "context",
			lineIndex,
			lineNumber:
				side === "deletions"
					? line.deletionLineNumber
					: line.additionLineNumber,
			sign: " ",
			text: line.text,
		};
	}
	if (line.kind === "delete") {
		return {
			kind: side === "deletions" ? "delete" : "empty",
			lineIndex: side === "deletions" ? lineIndex : undefined,
			lineNumber: side === "deletions" ? line.deletionLineNumber : undefined,
			sign: side === "deletions" ? "-" : " ",
			text: side === "deletions" ? line.text : "",
		};
	}
	return {
		kind: side === "additions" ? "add" : "empty",
		lineIndex: side === "additions" ? lineIndex : undefined,
		lineNumber: side === "additions" ? line.additionLineNumber : undefined,
		sign: side === "additions" ? "+" : " ",
		text: side === "additions" ? line.text : "",
	};
}

function buildUnifiedRows(hunk: ReviewHunk): UnifiedRow[] {
	return hunk.lines.map((line, index) => ({
		id: `${hunk.id}:unified:${index}`,
		lineIndex: index,
		kind: line.kind,
		deletionLineNumber: line.deletionLineNumber,
		additionLineNumber: line.additionLineNumber,
		sign: line.kind === "add" ? "+" : line.kind === "delete" ? "-" : " ",
		text: line.text,
	}));
}

type IndexedReviewLine = {
	index: number;
	line: ReviewLine;
};

function toCommentableLine(
	line: ReviewLine,
	index: number,
): ReviewDiffCommentableLine | null {
	if (line.kind === "delete" && line.deletionLineNumber != null) {
		return {
			index,
			side: "deletions",
			lineNumber: line.deletionLineNumber,
			text: line.text,
			kind: "delete",
		};
	}
	if (line.kind === "add" && line.additionLineNumber != null) {
		return {
			index,
			side: "additions",
			lineNumber: line.additionLineNumber,
			text: line.text,
			kind: "add",
		};
	}
	return null;
}

function buildSplitRows(hunk: ReviewHunk): SplitRow[] {
	const rows: SplitRow[] = [];
	let index = 0;
	while (index < hunk.lines.length) {
		const line = hunk.lines[index];
		if (line.kind === "context") {
			rows.push({
				id: `${hunk.id}:split:${index}`,
				deletion: cellForLine(line, index, "deletions"),
				addition: cellForLine(line, index, "additions"),
			});
			index += 1;
			continue;
		}

		const deletions: IndexedReviewLine[] = [];
		const additions: IndexedReviewLine[] = [];
		const startIndex = index;
		while (index < hunk.lines.length) {
			const current = hunk.lines[index];
			if (current.kind === "context") break;
			if (current.kind === "delete") deletions.push({ index, line: current });
			else additions.push({ index, line: current });
			index += 1;
		}

		for (
			let rowIndex = 0;
			rowIndex < Math.max(deletions.length, additions.length);
			rowIndex += 1
		) {
			const deletion = deletions[rowIndex];
			const addition = additions[rowIndex];
			rows.push({
				id: `${hunk.id}:split:${startIndex}:${rowIndex}`,
				deletion: deletion
					? cellForLine(deletion.line, deletion.index, "deletions")
					: { kind: "empty", sign: " ", text: "" },
				addition: addition
					? cellForLine(addition.line, addition.index, "additions")
					: { kind: "empty", sign: " ", text: "" },
			});
		}
	}
	return rows;
}

export function getReviewDiffCommentableLines(
	hunk: ReviewHunk,
	side?: ReviewDiffSide,
	view: ReviewDiffView = "unified",
): ReviewDiffCommentableLine[] {
	if (view === "split") {
		const lines: ReviewDiffCommentableLine[] = [];
		for (const row of buildSplitRows(hunk)) {
			for (const cell of [row.deletion, row.addition]) {
				if (cell.lineIndex == null || cell.lineNumber == null) continue;
				const sourceLine = hunk.lines[cell.lineIndex];
				const line = toCommentableLine(sourceLine, cell.lineIndex);
				if (!line || (side && line.side !== side)) continue;
				lines.push(line);
			}
		}
		return lines;
	}

	return hunk.lines.flatMap((line, index) => {
		const commentableLine = toCommentableLine(line, index);
		if (!commentableLine || (side && commentableLine.side !== side)) return [];
		return [commentableLine];
	});
}

const COMMENT_ANNOTATION_MIN_HEIGHT = 3;
const EDITING_COMMENT_MIN_HEIGHT = 4;
const COMMENT_ANNOTATION_MAX_HEIGHT = 12;
const ESTIMATED_COMMENT_WRAP_COLUMNS = 72;

function estimateTextLineCount(text: string): number {
	const lines = text.length > 0 ? text.split("\n") : [""];
	return lines.reduce(
		(count, line) =>
			count +
			Math.max(1, Math.ceil(line.length / ESTIMATED_COMMENT_WRAP_COLUMNS)),
		0,
	);
}

function annotationHeight(
	annotation: DiffLineAnnotation<ReviewDiffAnnotationMetadata> | undefined,
): number {
	if (!annotation) return 0;
	const contentLines = estimateTextLineCount(annotation.metadata.comment);
	const minHeight = annotation.metadata.editing
		? EDITING_COMMENT_MIN_HEIGHT
		: COMMENT_ANNOTATION_MIN_HEIGHT;
	const chromeHeight = annotation.metadata.editing ? 3 : 2;
	return Math.min(
		COMMENT_ANNOTATION_MAX_HEIGHT,
		Math.max(minHeight, contentLines + chromeHeight),
	);
}

type SplitAnnotationGroup = {
	deletions: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[];
	additions: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[];
};

function annotationLineIndex(
	hunk: ReviewHunk,
	annotation: DiffLineAnnotation<ReviewDiffAnnotationMetadata>,
): number | null {
	const line = getReviewDiffCommentableLines(hunk, annotation.side).find(
		(candidate) => candidate.lineNumber === annotation.lineNumber,
	);
	return line?.index ?? null;
}

function getUnifiedAnnotationsAfterRow(
	row: UnifiedRow,
	hunk: ReviewHunk,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[],
): DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] {
	if (row.lineIndex == null) return [];
	return annotations.filter(
		(annotation) => annotationLineIndex(hunk, annotation) === row.lineIndex,
	);
}

function getSplitAnnotationsAfterRow(
	row: SplitRow,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[],
): SplitAnnotationGroup {
	return {
		deletions: annotations.filter(
			(annotation) =>
				annotation.side === "deletions" &&
				row.deletion.lineNumber === annotation.lineNumber,
		),
		additions: annotations.filter(
			(annotation) =>
				annotation.side === "additions" &&
				row.addition.lineNumber === annotation.lineNumber,
		),
	};
}

function unifiedAnnotationOffsetBeforeLine(
	hunk: ReviewHunk,
	lineIndex: number,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[],
): number {
	return annotations.reduce((offset, annotation) => {
		const annotatedLineIndex = annotationLineIndex(hunk, annotation);
		if (annotatedLineIndex == null || annotatedLineIndex >= lineIndex) {
			return offset;
		}
		return offset + annotationHeight(annotation);
	}, 0);
}

function splitAnnotationOffsetBeforeRow(
	rows: SplitRow[],
	rowIndex: number,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[],
): number {
	let offset = 0;
	for (const row of rows.slice(0, rowIndex)) {
		const group = getSplitAnnotationsAfterRow(row, annotations);
		for (
			let index = 0;
			index < Math.max(group.deletions.length, group.additions.length);
			index += 1
		) {
			offset += Math.max(
				annotationHeight(group.deletions[index]),
				annotationHeight(group.additions[index]),
			);
		}
	}
	return offset;
}

export function getReviewDiffLineTop(
	hunk: ReviewHunk,
	lineIndex: number,
	view: ReviewDiffView,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] = [],
): number {
	if (view === "unified") {
		return (
			lineIndex +
			unifiedAnnotationOffsetBeforeLine(hunk, lineIndex, annotations)
		);
	}
	const rows = buildSplitRows(hunk);
	const rowIndex = rows.findIndex(
		(row) =>
			row.deletion.lineIndex === lineIndex ||
			row.addition.lineIndex === lineIndex,
	);
	if (rowIndex < 0) return lineIndex;
	return rowIndex + splitAnnotationOffsetBeforeRow(rows, rowIndex, annotations);
}

export function getReviewDiffRangeBounds(
	hunk: ReviewHunk,
	range: ReviewDiffLineRange,
	view: ReviewDiffView,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] = [],
): ReviewDiffVisualBounds | null {
	const tops = getReviewDiffCommentableLines(hunk, range.side, view).flatMap(
		(line) => {
			if (
				line.lineNumber < range.startLine ||
				line.lineNumber > range.endLine
			) {
				return [];
			}
			return [getReviewDiffLineTop(hunk, line.index, view, annotations)];
		},
	);
	if (tops.length === 0) return null;
	const top = Math.min(...tops);
	const bottom = Math.max(...tops);
	return { top, height: bottom - top + 1 };
}

function cursorBackgroundForKind(kind: DiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.diffCursorAddedBg;
		case "delete":
			return theme.diffCursorRemovedBg;
		default:
			return theme.diffCursorBg;
	}
}

function backgroundForKind(kind: DiffCellKind): string {
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

function contentBackgroundForKind(kind: DiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.diffAddedContentBg;
		case "delete":
			return theme.diffRemovedContentBg;
		default:
			return backgroundForKind(kind);
	}
}

function signColorForKind(kind: DiffCellKind): string {
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

function textColorForKind(kind: DiffCellKind): string {
	if (kind === "metadata") return theme.metaText;
	if (kind === "empty") return theme.textPlaceholder;
	return theme.textPrimary;
}

function renderContentText(
	text: string,
	kind: DiffCellKind,
	filetype: string | undefined,
	backgroundColor?: Accessor<string>,
) {
	const bg = () => backgroundColor?.() ?? contentBackgroundForKind(kind);
	if (filetype && kind !== "metadata" && kind !== "empty") {
		return (
			<code
				content={text}
				filetype={filetype}
				syntaxStyle={syntaxStyle()}
				fg={textColorForKind(kind)}
				bg={bg()}
				wrapMode="none"
				flexGrow={1}
				height={1}
				flexShrink={0}
			/>
		);
	}
	return (
		<text
			fg={textColorForKind(kind)}
			bg={bg()}
			flexGrow={1}
			height={1}
			flexShrink={0}
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
			height={annotationHeight(annotation)}
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
		<box height={annotationHeight(annotation)} flexShrink={0} width="100%">
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
	group: SplitAnnotationGroup,
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
					annotationHeight(deletion),
					annotationHeight(addition),
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
	row: UnifiedRow,
	lineNumberWidth: number,
	filetype: string | undefined,
	hunk?: ReviewHunk,
	isActiveLine?: (key: string) => boolean,
	onLineMouseDown?: ReviewDiffBlockProps["onLineMouseDown"],
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
			? toCommentableLine(hunk.lines[row.lineIndex], row.lineIndex)
			: null;
	const handleMouseDown = (event: TuiMouseEvent) => {
		const line = commentableLine();
		if (!line) return;
		onLineMouseDown?.(line, event);
	};
	return (
		<box
			id={
				active() && hunk && row.lineIndex != null
					? `review-line-cursor-${hunk.id}-${row.lineIndex}`
					: undefined
			}
			flexDirection="row"
			backgroundColor={bg()}
			height={1}
			flexShrink={0}
			onMouseDown={handleMouseDown}
		>
			<text
				fg={theme.textMuted}
				bg={active() ? theme.diffCursorGutterBg : bg()}
			>
				{formatLineNumber(row.deletionLineNumber, lineNumberWidth)}
			</text>
			<text
				fg={theme.textMuted}
				bg={active() ? theme.diffCursorGutterBg : bg()}
			>
				{" "}
				{formatLineNumber(row.additionLineNumber, lineNumberWidth)}
			</text>
			<text fg={signColorForKind(row.kind)} bg={bg()}>
				{row.sign}{" "}
			</text>
			{renderContentText(row.text, row.kind, filetype, bg)}
		</box>
	);
}

function renderSplitCell(
	cell: DiffCell,
	lineNumberWidth: number,
	filetype: string | undefined,
	hunk: ReviewHunk,
	isActiveLine?: (key: string) => boolean,
	onLineMouseDown?: ReviewDiffBlockProps["onLineMouseDown"],
) {
	const commentableLine = () =>
		cell.lineIndex != null
			? toCommentableLine(hunk.lines[cell.lineIndex], cell.lineIndex)
			: null;
	const active = () => {
		const line = commentableLine();
		return (
			line != null &&
			(isActiveLine?.(`line:${line.index}:${line.side}`) ?? false)
		);
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
			id={
				active() ? `review-line-cursor-${hunk.id}-${cell.lineIndex}` : undefined
			}
			width="50%"
			flexDirection="row"
			backgroundColor={bg()}
			height={1}
			flexShrink={0}
			onMouseDown={handleMouseDown}
		>
			<text
				fg={theme.textMuted}
				bg={active() ? theme.diffCursorGutterBg : bg()}
			>
				{formatLineNumber(cell.lineNumber, lineNumberWidth)}
			</text>
			<text fg={signColorForKind(cell.kind)} bg={bg()}>
				{cell.sign}{" "}
			</text>
			{renderContentText(cell.text, cell.kind, filetype, bg)}
		</box>
	);
}

function rawPatchRows(rawPatch: string): UnifiedRow[] {
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
								<For each={buildUnifiedRows(currentHunk())}>
									{(row) => (
										<>
											{renderUnifiedRow(
												row,
												lineNumberWidth(),
												props.filetype,
												currentHunk(),
												isActiveUnifiedLine,
												props.onLineMouseDown,
											)}
											<For
												each={getUnifiedAnnotationsAfterRow(
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
										</>
									)}
								</For>
							</box>
						}
					>
						<box flexDirection="column" gap={0}>
							<For each={buildSplitRows(currentHunk())}>
								{(row) => (
									<>
										<box
											flexDirection="row"
											width="100%"
											height={1}
											flexShrink={0}
										>
											{renderSplitCell(
												row.deletion,
												lineNumberWidth(),
												props.filetype,
												currentHunk(),
												isActiveSplitLine,
												props.onLineMouseDown,
											)}
											{renderSplitCell(
												row.addition,
												lineNumberWidth(),
												props.filetype,
												currentHunk(),
												isActiveSplitLine,
												props.onLineMouseDown,
											)}
										</box>
										{renderSplitAnnotationRows(
											getSplitAnnotationsAfterRow(row, annotations()),
											props.annotationEditor,
										)}
									</>
								)}
							</For>
						</box>
					</Show>
				);
			}}
		</Show>
	);
}
