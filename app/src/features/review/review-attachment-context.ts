import type {
	CodeReviewCommentRange,
	CodeReviewMessagePart,
} from "../../messages/parts";
import type { ReviewLine } from "../../shell/diff/types";
import {
	loadReviewFiles,
	loadReviewFilesForRevisions,
	type ReviewFile,
} from "./model";

export type ReviewAttachmentContextKind = "exact" | "live" | "unavailable";

export type ReviewAttachmentContext = {
	kind: ReviewAttachmentContextKind;
	files: Map<string, ReviewFile>;
};

export type ReviewRangeExcerpt = {
	header: string;
	lines: ReviewLine[];
	truncatedBefore: boolean;
	truncatedAfter: boolean;
};

export async function loadReviewAttachmentContext(options: {
	review: CodeReviewMessagePart["review"];
	draft: boolean;
	cwd?: string;
}): Promise<ReviewAttachmentContext> {
	const commit = options.review.commit;
	if (commit) {
		const files = await loadReviewFilesForRevisions(
			options.cwd,
			commit.parentSha,
			commit.sha,
		);
		const filesByPath = new Map(files.map((file) => [file.path, file]));
		const hasReviewedFile = options.review.files.some((file) =>
			filesByPath.has(file.path),
		);
		return {
			kind: hasReviewedFile ? "exact" : "unavailable",
			files: filesByPath,
		};
	}
	if (options.draft) {
		const files = await loadReviewFiles(options.cwd, { kind: "working" });
		return {
			kind: "live",
			files: new Map(files.map((file) => [file.path, file])),
		};
	}
	return { kind: "unavailable", files: new Map() };
}

function lineNumberForSide(
	line: ReviewLine,
	side: CodeReviewCommentRange["side"],
): number | undefined {
	return side === "additions"
		? line.additionLineNumber
		: line.deletionLineNumber;
}

export function extractReviewRangeExcerpt(
	file: ReviewFile | undefined,
	range: CodeReviewCommentRange,
	contextLines = 3,
): ReviewRangeExcerpt | null {
	if (!file) return null;
	const startLine = Math.min(range.startLine, range.endLine);
	const endLine = Math.max(range.startLine, range.endLine);

	for (const hunk of file.hunks) {
		let first = -1;
		let last = -1;
		for (const [index, line] of hunk.lines.entries()) {
			const lineNumber = lineNumberForSide(line, range.side);
			if (
				lineNumber === undefined ||
				lineNumber < startLine ||
				lineNumber > endLine
			) {
				continue;
			}
			if (first < 0) first = index;
			last = index;
		}
		if (first < 0 || last < 0) continue;

		const excerptStart = Math.max(0, first - contextLines);
		const excerptEnd = Math.min(hunk.lines.length, last + contextLines + 1);
		return {
			header: hunk.header,
			lines: hunk.lines.slice(excerptStart, excerptEnd),
			truncatedBefore: excerptStart > 0,
			truncatedAfter: excerptEnd < hunk.lines.length,
		};
	}
	return null;
}
