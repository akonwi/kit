import type { CodeReviewFileComment } from "../../messages/parts";
import type { CodeReviewSubmission } from "../code-review/attachment";
import type { ReviewFile, ReviewHunk } from "./model";

export type ReviewDraftState = {
	fileNotes: Map<string, string>;
	hunkNotes: Map<string, string>;
};

function normalizeNote(note: string | undefined): string {
	return note?.trim() ?? "";
}

function buildHunkRanges(
	hunk: ReviewHunk,
	comment: string,
): CodeReviewFileComment["ranges"] {
	if (hunk.additionCount > 0) {
		return [
			{
				side: "additions",
				startLine: hunk.additionStart,
				endLine: hunk.additionStart + hunk.additionCount - 1,
				comment,
			},
		];
	}
	if (hunk.deletionCount > 0) {
		return [
			{
				side: "deletions",
				startLine: hunk.deletionStart,
				endLine: hunk.deletionStart + hunk.deletionCount - 1,
				comment,
			},
		];
	}
	return [];
}

export function countDraftNotes(state: ReviewDraftState): number {
	let count = 0;
	for (const note of state.fileNotes.values()) {
		if (normalizeNote(note)) count += 1;
	}
	for (const note of state.hunkNotes.values()) {
		if (normalizeNote(note)) count += 1;
	}
	return count;
}

export function countFileDraftNotes(
	file: ReviewFile,
	state: ReviewDraftState,
): number {
	let count = normalizeNote(state.fileNotes.get(file.noteKey)) ? 1 : 0;
	for (const hunk of file.hunks) {
		if (normalizeNote(state.hunkNotes.get(hunk.noteKey))) count += 1;
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
		const ranges = file.hunks.flatMap((hunk) => {
			const note = normalizeNote(state.hunkNotes.get(hunk.noteKey));
			if (!note) return [];
			return buildHunkRanges(hunk, note);
		});
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
