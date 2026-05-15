import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import type { DiffLineAnnotation } from "@pierre/diffs";
import { For, Show } from "solid-js";
import type { ReviewDiffView } from "../../settings";
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
};

export type ReviewDiffAnnotationMarker = ReviewDiffVisualBounds & {
	key: string;
	side: ReviewDiffSide;
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

export function getReviewDiffLineTop(
	hunk: ReviewHunk,
	lineIndex: number,
	view: ReviewDiffView,
): number {
	if (view === "unified") return lineIndex;
	const rowIndex = buildSplitRows(hunk).findIndex(
		(row) =>
			row.deletion.lineIndex === lineIndex ||
			row.addition.lineIndex === lineIndex,
	);
	return rowIndex >= 0 ? rowIndex : lineIndex;
}

export function getReviewDiffRangeBounds(
	hunk: ReviewHunk,
	range: ReviewDiffLineRange,
	view: ReviewDiffView,
): ReviewDiffVisualBounds | null {
	const tops = getReviewDiffCommentableLines(hunk, range.side, view).flatMap(
		(line) => {
			if (
				line.lineNumber < range.startLine ||
				line.lineNumber > range.endLine
			) {
				return [];
			}
			return [getReviewDiffLineTop(hunk, line.index, view)];
		},
	);
	if (tops.length === 0) return null;
	const top = Math.min(...tops);
	const bottom = Math.max(...tops);
	return { top, height: bottom - top + 1 };
}

export function getReviewDiffAnnotationMarkers(
	hunk: ReviewHunk,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[],
	view: ReviewDiffView,
): ReviewDiffAnnotationMarker[] {
	const markerRows = new Map<
		string,
		{ side: ReviewDiffSide; rows: number[] }
	>();
	for (const annotation of annotations) {
		const line = getReviewDiffCommentableLines(
			hunk,
			annotation.side,
			view,
		).find((candidate) => candidate.lineNumber === annotation.lineNumber);
		if (!line) continue;
		const existing = markerRows.get(annotation.metadata.key) ?? {
			side: annotation.side,
			rows: [],
		};
		existing.rows.push(getReviewDiffLineTop(hunk, line.index, view));
		markerRows.set(annotation.metadata.key, existing);
	}
	return Array.from(markerRows.entries()).map(([key, marker]) => {
		const top = Math.min(...marker.rows);
		const bottom = Math.max(...marker.rows);
		return { key, side: marker.side, top, height: bottom - top + 1 };
	});
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
) {
	const bg = () => contentBackgroundForKind(kind);
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

function renderUnifiedRow(
	row: UnifiedRow,
	lineNumberWidth: number,
	filetype: string | undefined,
	hunk?: ReviewHunk,
	onLineMouseDown?: ReviewDiffBlockProps["onLineMouseDown"],
) {
	const bg = () => backgroundForKind(row.kind);
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
			flexDirection="row"
			backgroundColor={bg()}
			height={1}
			flexShrink={0}
			onMouseDown={handleMouseDown}
		>
			<text fg={theme.textMuted} bg={bg()}>
				{formatLineNumber(row.deletionLineNumber, lineNumberWidth)}
			</text>
			<text fg={theme.textMuted} bg={bg()}>
				{" "}
				{formatLineNumber(row.additionLineNumber, lineNumberWidth)}
			</text>
			<text fg={signColorForKind(row.kind)} bg={bg()}>
				{row.sign}{" "}
			</text>
			{renderContentText(row.text, row.kind, filetype)}
		</box>
	);
}

function renderSplitCell(
	cell: DiffCell,
	lineNumberWidth: number,
	filetype: string | undefined,
	hunk: ReviewHunk,
	onLineMouseDown?: ReviewDiffBlockProps["onLineMouseDown"],
) {
	const bg = () => backgroundForKind(cell.kind);
	const commentableLine = () =>
		cell.lineIndex != null
			? toCommentableLine(hunk.lines[cell.lineIndex], cell.lineIndex)
			: null;
	const handleMouseDown = (event: TuiMouseEvent) => {
		const line = commentableLine();
		if (!line) return;
		onLineMouseDown?.(line, event);
	};
	return (
		<box
			width="50%"
			flexDirection="row"
			backgroundColor={bg()}
			height={1}
			flexShrink={0}
			onMouseDown={handleMouseDown}
		>
			<text fg={theme.textMuted} bg={bg()}>
				{formatLineNumber(cell.lineNumber, lineNumberWidth)}
			</text>
			<text fg={signColorForKind(cell.kind)} bg={bg()}>
				{cell.sign}{" "}
			</text>
			{renderContentText(cell.text, cell.kind, filetype)}
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
				const lineNumberWidth = () => getLineNumberWidth(hunk());
				return (
					<Show
						when={props.view === "split"}
						fallback={
							<box flexDirection="column" gap={0}>
								<For each={buildUnifiedRows(hunk())}>
									{(row) =>
										renderUnifiedRow(
											row,
											lineNumberWidth(),
											props.filetype,
											hunk(),
											props.onLineMouseDown,
										)
									}
								</For>
							</box>
						}
					>
						<box flexDirection="column" gap={0}>
							<For each={buildSplitRows(hunk())}>
								{(row) => (
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
											hunk(),
											props.onLineMouseDown,
										)}
										{renderSplitCell(
											row.addition,
											lineNumberWidth(),
											props.filetype,
											hunk(),
											props.onLineMouseDown,
										)}
									</box>
								)}
							</For>
						</box>
					</Show>
				);
			}}
		</Show>
	);
}
