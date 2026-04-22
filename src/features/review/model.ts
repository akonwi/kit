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
	noteKey: string;
	header: string;
	context: string;
	lines: ReviewLine[];
	changeCount: number;
	patchStartLine: number;
	patchLineCount: number;
};

export type ReviewFile = {
	id: string;
	noteKey: string;
	path: string;
	prevPath?: string;
	status: FileDiffMetadata["type"];
	filetype?: string;
	rawPatch: string;
	hunks: ReviewHunk[];
	changeCount: number;
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

function splitRawDiffIntoFiles(diff: string): string[] {
	if (!diff.trim()) return [];
	return diff
		.split(/(?=^diff --git )/m)
		.filter((chunk) => chunk.trim().startsWith("diff --git "));
}

function inferFiletype(path: string): string | undefined {
	const normalized = path.toLowerCase();
	if (normalized.endsWith(".ts") || normalized.endsWith(".tsx")) {
		return "typescript";
	}
	if (
		normalized.endsWith(".js") ||
		normalized.endsWith(".jsx") ||
		normalized.endsWith(".mjs")
	) {
		return "javascript";
	}
	if (normalized.endsWith(".json")) return "json";
	if (normalized.endsWith(".md")) return "markdown";
	if (normalized.endsWith(".sh")) return "bash";
	if (normalized.endsWith(".yml") || normalized.endsWith(".yaml"))
		return "yaml";
	if (normalized.endsWith(".css")) return "css";
	if (normalized.endsWith(".html")) return "html";
	if (normalized.endsWith(".rs")) return "rust";
	if (normalized.endsWith(".go")) return "go";
	if (normalized.endsWith(".py")) return "python";
	return undefined;
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

function getRenderedUnifiedLineCount(hunk: Hunk): number {
	return hunk.hunkContent.reduce((count, block) => {
		if (block.type === "context") return count + block.lines;
		return count + block.additions + block.deletions;
	}, 0);
}

function hunkToReviewHunk(
	file: FileDiffMetadata,
	hunk: Hunk,
	fileNoteKey: string,
	index: number,
	renderedStartLine: number,
): ReviewHunk {
	const lines = hunk.hunkContent.flatMap((block) =>
		block.type === "context"
			? contextLines(block, file)
			: changeLines(block, file),
	);
	const changeCount = lines.filter((line) => line.kind !== "context").length;
	const noteKey = `${fileNoteKey}:${hunk.hunkSpecs ?? `hunk-${index + 1}`}:${index}`;
	const header = hunk.hunkSpecs ?? `Hunk ${index + 1}`;
	const patchLineCount = getRenderedUnifiedLineCount(hunk);
	return {
		id: `${file.name}:${hunk.hunkSpecs ?? index}:${index}`,
		noteKey,
		header,
		context: hunk.hunkContext ?? "",
		lines,
		changeCount,
		patchStartLine: renderedStartLine,
		patchLineCount,
	};
}

function fileToReviewFile(
	file: FileDiffMetadata,
	rawPatch: string,
	index: number,
): ReviewFile {
	const noteKey = `${file.prevName ?? ""}->${file.name}`;
	let renderedStartLine = 0;
	const hunks = file.hunks.map((hunk, hunkIndex) => {
		const reviewHunk = hunkToReviewHunk(
			file,
			hunk,
			noteKey,
			hunkIndex,
			renderedStartLine,
		);
		renderedStartLine += getRenderedUnifiedLineCount(hunk);
		return reviewHunk;
	});
	const changeCount = hunks.reduce((sum, hunk) => sum + hunk.changeCount, 0);
	return {
		id: `${noteKey}:${index}`,
		noteKey,
		path: file.name,
		prevPath: file.prevName,
		status: file.type,
		filetype: inferFiletype(file.name),
		rawPatch,
		hunks,
		changeCount,
	};
}

export async function loadReviewFiles(): Promise<ReviewFile[]> {
	const diff = getWorkingTreeDiff();
	if (!diff.trim()) return [];
	const parsed = parsePatchFiles(diff, "review", true);
	const rawFiles = splitRawDiffIntoFiles(diff);
	const files = parsed.flatMap((patch) => patch.files);
	return files.map((file, index) =>
		fileToReviewFile(file, rawFiles[index] ?? "", index),
	);
}
