/**
 * Core diff data types used by the shared DiffBlock renderer.
 *
 * These were originally named `ReviewLine`/`ReviewHunk` and lived in
 * `features/review/model.ts`. They are kept with the same names here so
 * the review feature can re-export them without churn; they are pure
 * data types describing a unified-diff hunk and apply equally to any
 * caller that wants to render a diff.
 */

export type ReviewLine = {
	kind: "add" | "context" | "delete";
	text: string;
	additionLineNumber?: number;
	deletionLineNumber?: number;
};

export type ReviewHunk = {
	id: string;
	noteKey: string;
	header: string;
	context: string;
	lines: ReviewLine[];
	/** Offset of lines[0] in the source hunk's unified row space. */
	lineIndexOffset?: number;
	lineWindow?: { start: number; end: number; total: number };
	changeCount: number;
	rawPatch: string;
	patchStartLine: number;
	patchLineCount: number;
	additionStart: number;
	additionCount: number;
	deletionStart: number;
	deletionCount: number;
	collapsedBefore: number;
};
