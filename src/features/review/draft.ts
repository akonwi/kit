import type { CodeReviewFileComment } from "../../messages/parts";
import type { CodeReviewSubmission } from "../code-review/attachment";
import type { ReviewFile } from "./model";

export type ReviewDraftState = {
	fileNotes: Map<string, string>;
	rangeNotes: Map<string, string>;
};

export type ReviewRangeDraft = {
	path: string;
	side: "additions" | "deletions";
	startLine: number;
	endLine: number;
};

export function buildRangeNoteKey(range: ReviewRangeDraft): string {
	const startLine = Math.min(range.startLine, range.endLine);
	const endLine = Math.max(range.startLine, range.endLine);
	return `${range.path}::${range.side}::${startLine}-${endLine}`;
}

export function parseRangeNoteKey(key: string): ReviewRangeDraft | null {
	const [path, side, range] = key.split("::");
	if (!path || !side || !range) return null;
	if (side !== "additions" && side !== "deletions") return null;
	const [startLineText, endLineText] = range.split("-");
	const startLine = Number(startLineText);
	const endLine = Number(endLineText);
	if (!Number.isFinite(startLine) || !Number.isFinite(endLine)) return null;
	return {
		path,
		side,
		startLine: Math.min(startLine, endLine),
		endLine: Math.max(startLine, endLine),
	};
}

function normalizeNote(note: string | undefined): string {
	return note?.trim() ?? "";
}

function countNonEmptyNotes(notes: Iterable<string>): number {
	let count = 0;
	for (const note of notes) {
		if (normalizeNote(note)) count += 1;
	}
	return count;
}

export function countDraftNotes(state: ReviewDraftState): number {
	return (
		countNonEmptyNotes(state.fileNotes.values()) +
		countNonEmptyNotes(state.rangeNotes.values())
	);
}

export function countFileDraftNotes(
	file: ReviewFile,
	state: ReviewDraftState,
): number {
	let count = normalizeNote(state.fileNotes.get(file.noteKey)) ? 1 : 0;
	for (const [key, value] of state.rangeNotes) {
		if (key.startsWith(`${file.path}::`) && normalizeNote(value)) count += 1;
	}
	return count;
}

export function buildReviewSubmission(
	files: ReviewFile[],
	state: ReviewDraftState,
): CodeReviewSubmission | null {
	const submittedFiles: CodeReviewFileComment[] = [];

	for (const file of files) {
		const fileComment = normalizeNote(state.fileNotes.get(file.noteKey));
		const ranges = [
			...Array.from(state.rangeNotes.entries())
				.filter(
					([key, value]) =>
						key.startsWith(`${file.path}::`) && normalizeNote(value).length > 0,
				)
				.flatMap(([key, value]) => {
					const parsed = parseRangeNoteKey(key);
					if (!parsed) return [];
					return [
						{
							side: parsed.side as CodeReviewFileComment["ranges"][number]["side"],
							startLine: parsed.startLine,
							endLine: parsed.endLine,
							comment: normalizeNote(value),
						},
					];
				}),
		];
		if (!fileComment && ranges.length === 0) continue;
		submittedFiles.push({
			path: file.path,
			fileComment,
			ranges,
		});
	}

	if (submittedFiles.length === 0) return null;
	return {
		submittedAt: new Date().toISOString(),
		files: submittedFiles,
	};
}
