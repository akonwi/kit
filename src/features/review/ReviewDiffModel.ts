import type { DiffLineAnnotation } from "@pierre/diffs";
import type { ReviewDiffView } from "../../settings";
import type { ReviewHunk, ReviewLine } from "./model";

export type ReviewDiffSide = "additions" | "deletions";
export type ReviewDiffCellKind =
	| "add"
	| "context"
	| "delete"
	| "empty"
	| "metadata";

export type ReviewDiffCell = {
	kind: ReviewDiffCellKind;
	lineIndex?: number;
	lineNumber?: number;
	sign: string;
	text: string;
};

export type ReviewDiffSplitRow = {
	id: string;
	deletion: ReviewDiffCell;
	addition: ReviewDiffCell;
};

export type ReviewDiffUnifiedRow = {
	id: string;
	lineIndex?: number;
	kind: Exclude<ReviewDiffCellKind, "empty">;
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

export function getReviewDiffActiveLineId(
	hunkId: string,
	lineIndex: number,
): string {
	return `review-line-cursor-${hunkId}-${lineIndex}`;
}

function getHunkLineIndexOffset(hunk: ReviewHunk): number {
	return hunk.lineIndexOffset ?? 0;
}

function getLocalLineIndex(hunk: ReviewHunk, lineIndex: number): number {
	return lineIndex - getHunkLineIndexOffset(hunk);
}

function cellForLine(
	line: ReviewLine,
	lineIndex: number,
	side: ReviewDiffSide,
): ReviewDiffCell {
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

export function buildReviewDiffUnifiedRows(
	hunk: ReviewHunk,
): ReviewDiffUnifiedRow[] {
	const lineIndexOffset = getHunkLineIndexOffset(hunk);
	return hunk.lines.map((line, index) => ({
		id: `${hunk.id}:unified:${lineIndexOffset + index}`,
		lineIndex: lineIndexOffset + index,
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

export function getReviewDiffCommentableLine(
	hunk: ReviewHunk,
	lineIndex: number,
): ReviewDiffCommentableLine | null {
	const line = hunk.lines[getLocalLineIndex(hunk, lineIndex)];
	if (!line) return null;
	return toCommentableLine(line, lineIndex);
}

export function buildReviewDiffSplitRows(
	hunk: ReviewHunk,
): ReviewDiffSplitRow[] {
	const rows: ReviewDiffSplitRow[] = [];
	const lineIndexOffset = getHunkLineIndexOffset(hunk);
	let index = 0;
	while (index < hunk.lines.length) {
		const line = hunk.lines[index];
		const globalIndex = lineIndexOffset + index;
		if (line.kind === "context") {
			rows.push({
				id: `${hunk.id}:split:${globalIndex}`,
				deletion: cellForLine(line, globalIndex, "deletions"),
				addition: cellForLine(line, globalIndex, "additions"),
			});
			index += 1;
			continue;
		}

		const deletions: IndexedReviewLine[] = [];
		const additions: IndexedReviewLine[] = [];
		const startIndex = index;
		const globalStartIndex = lineIndexOffset + startIndex;
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
				id: `${hunk.id}:split:${globalStartIndex}:${rowIndex}`,
				deletion: deletion
					? cellForLine(
							deletion.line,
							lineIndexOffset + deletion.index,
							"deletions",
						)
					: { kind: "empty", sign: " ", text: "" },
				addition: addition
					? cellForLine(
							addition.line,
							lineIndexOffset + addition.index,
							"additions",
						)
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
		for (const row of buildReviewDiffSplitRows(hunk)) {
			for (const cell of [row.deletion, row.addition]) {
				if (cell.lineIndex == null || cell.lineNumber == null) continue;
				const sourceLine = hunk.lines[getLocalLineIndex(hunk, cell.lineIndex)];
				if (!sourceLine) continue;
				const line = toCommentableLine(sourceLine, cell.lineIndex);
				if (!line || (side && line.side !== side)) continue;
				lines.push(line);
			}
		}
		return lines;
	}

	const lineIndexOffset = getHunkLineIndexOffset(hunk);
	return hunk.lines.flatMap((line, index) => {
		const commentableLine = toCommentableLine(line, lineIndexOffset + index);
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

export function getReviewDiffAnnotationHeight(
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

export type ReviewDiffSplitAnnotationGroup = {
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

export function getReviewDiffUnifiedAnnotationsAfterRow(
	row: ReviewDiffUnifiedRow,
	hunk: ReviewHunk,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[],
): DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] {
	if (row.lineIndex == null) return [];
	return annotations.filter(
		(annotation) => annotationLineIndex(hunk, annotation) === row.lineIndex,
	);
}

export function getReviewDiffSplitAnnotationsAfterRow(
	row: ReviewDiffSplitRow,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[],
): ReviewDiffSplitAnnotationGroup {
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
		return offset + getReviewDiffAnnotationHeight(annotation);
	}, 0);
}

function splitAnnotationOffsetBeforeRow(
	rows: ReviewDiffSplitRow[],
	rowIndex: number,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[],
): number {
	let offset = 0;
	for (const row of rows.slice(0, rowIndex)) {
		const group = getReviewDiffSplitAnnotationsAfterRow(row, annotations);
		for (
			let index = 0;
			index < Math.max(group.deletions.length, group.additions.length);
			index += 1
		) {
			offset += Math.max(
				getReviewDiffAnnotationHeight(group.deletions[index]),
				getReviewDiffAnnotationHeight(group.additions[index]),
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
			getLocalLineIndex(hunk, lineIndex) +
			unifiedAnnotationOffsetBeforeLine(hunk, lineIndex, annotations)
		);
	}
	const rows = buildReviewDiffSplitRows(hunk);
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
