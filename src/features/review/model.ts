import { spawnSync } from "node:child_process";
import type {
	ChangeContent,
	ContextContent,
	FileDiffMetadata,
	Hunk,
} from "@pierre/diffs";
import { parsePatchFiles } from "@pierre/diffs";

export type ReviewLine = {
	kind: "add" | "context" | "delete";
	text: string;
};

export type ReviewHunk = {
	id: string;
	header: string;
	context: string;
	lines: ReviewLine[];
};

export type ReviewFile = {
	path: string;
	prevPath?: string;
	status: FileDiffMetadata["type"];
	hunks: ReviewHunk[];
};

function getWorkingTreeDiff(): string {
	const staged = spawnSync(
		"git",
		[
			"diff",
			"--cached",
			"--no-ext-diff",
			"--find-renames",
			"--find-copies",
			"--unified=3",
		],
		{ encoding: "utf8" },
	);
	if (staged.status !== 0) {
		throw new Error(staged.stderr || "Failed to read staged diff.");
	}

	const unstaged = spawnSync(
		"git",
		["diff", "--no-ext-diff", "--find-renames", "--find-copies", "--unified=3"],
		{ encoding: "utf8" },
	);
	if (unstaged.status !== 0) {
		throw new Error(unstaged.stderr || "Failed to read unstaged diff.");
	}

	return [staged.stdout, unstaged.stdout]
		.filter((value) => value.trim().length > 0)
		.join("\n");
}

function contextLines(
	block: ContextContent,
	file: FileDiffMetadata,
): ReviewLine[] {
	return file.additionLines
		.slice(block.additionLineIndex, block.additionLineIndex + block.lines)
		.map((text) => ({ kind: "context" as const, text }));
}

function changeLines(
	block: ChangeContent,
	file: FileDiffMetadata,
): ReviewLine[] {
	const deleted = file.deletionLines
		.slice(block.deletionLineIndex, block.deletionLineIndex + block.deletions)
		.map((text) => ({ kind: "delete" as const, text }));
	const added = file.additionLines
		.slice(block.additionLineIndex, block.additionLineIndex + block.additions)
		.map((text) => ({ kind: "add" as const, text }));
	return [...deleted, ...added];
}

function hunkToReviewHunk(
	file: FileDiffMetadata,
	hunk: Hunk,
	index: number,
): ReviewHunk {
	const lines = hunk.hunkContent.flatMap((block) =>
		block.type === "context"
			? contextLines(block, file)
			: changeLines(block, file),
	);
	return {
		id: `${file.name}:${hunk.hunkSpecs ?? index}:${index}`,
		header: hunk.hunkSpecs ?? `Hunk ${index + 1}`,
		context: hunk.hunkContext ?? "",
		lines,
	};
}

function fileToReviewFile(file: FileDiffMetadata): ReviewFile {
	return {
		path: file.name,
		prevPath: file.prevName,
		status: file.type,
		hunks: file.hunks.map((hunk, index) => hunkToReviewHunk(file, hunk, index)),
	};
}

export async function loadReviewFiles(): Promise<ReviewFile[]> {
	const diff = getWorkingTreeDiff();
	if (!diff.trim()) return [];
	const parsed = parsePatchFiles(diff, "review", true);
	return parsed.flatMap((patch) => patch.files.map(fileToReviewFile));
}
