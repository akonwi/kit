import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
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
	rawPatch: string;
	patchStartLine: number;
	patchLineCount: number;
	additionStart: number;
	additionCount: number;
	deletionStart: number;
	deletionCount: number;
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

function runGit(
	cwd: string | undefined,
	args: string[],
	errorMessage: string,
): string {
	const result = spawnSync("git", args, { encoding: "utf8", cwd });
	if (result.status !== 0) {
		throw new Error(result.stderr || errorMessage);
	}
	return result.stdout;
}

function getWorkingTreeDiff(cwd?: string): string {
	const staged = runGit(
		cwd,
		[
			"diff",
			"--cached",
			"--no-ext-diff",
			"--find-renames",
			"--find-copies",
			"--unified=3",
		],
		"Failed to read staged diff.",
	);

	const unstaged = runGit(
		cwd,
		["diff", "--no-ext-diff", "--find-renames", "--find-copies", "--unified=3"],
		"Failed to read unstaged diff.",
	);

	const untracked = getUntrackedDiff(cwd);

	return [staged, unstaged, untracked]
		.filter((value) => value.trim().length > 0)
		.join("\n");
}

function getUntrackedDiff(cwd?: string): string {
	const output = runGit(
		cwd,
		["ls-files", "--others", "--exclude-standard", "-z"],
		"Failed to list untracked files.",
	);
	const paths = output.split("\0").filter(Boolean);
	if (paths.length === 0) return "";
	const repoRoot = runGit(
		cwd,
		["rev-parse", "--show-toplevel"],
		"Failed to resolve repository root.",
	).trim();
	return paths
		.map((filePath) => buildUntrackedFilePatch(repoRoot, filePath))
		.filter((patch): patch is string => patch !== null)
		.join("\n");
}

function buildUntrackedFilePatch(
	repoRoot: string,
	relativePath: string,
): string | null {
	const absolutePath = path.join(repoRoot, relativePath);
	if (!existsSync(absolutePath)) return null;

	let content: string;
	try {
		content = readFileSync(absolutePath, "utf8");
	} catch {
		return null;
	}
	if (content.includes("\0")) return null;

	const normalized = content.replace(/\r\n/g, "\n");
	const lines = normalized.length === 0 ? [] : normalized.split("\n");
	const lineCount = lines.length;
	const displayPath = relativePath.replace(/\\/g, "/");
	const body = lines.map((line) => `+${line}`).join("\n");
	return [
		`diff --git a/${displayPath} b/${displayPath}`,
		"new file mode 100644",
		"index 0000000..0000000",
		`--- /dev/null`,
		`+++ b/${displayPath}`,
		`@@ -0,0 +1,${lineCount} @@`,
		body,
	]
		.filter((line) => line.length > 0)
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
	rawPatch: string,
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
		rawPatch,
		patchStartLine: renderedStartLine,
		patchLineCount,
		additionStart: hunk.additionStart,
		additionCount: hunk.additionCount,
		deletionStart: hunk.deletionStart,
		deletionCount: hunk.deletionCount,
	};
}

function splitRawPatchIntoHunks(rawPatch: string): string[] {
	const lines = rawPatch.replace(/\r\n/g, "\n").split("\n");
	const firstHunkIndex = lines.findIndex((line) => line.startsWith("@@ "));
	if (firstHunkIndex < 0) return [];
	const headerLines = lines.slice(0, firstHunkIndex);
	const hunks: string[] = [];
	let current: string[] = [];
	for (const line of lines.slice(firstHunkIndex)) {
		if (line.startsWith("@@ ") && current.length > 0) {
			hunks.push([...headerLines, ...current].join("\n"));
			current = [line];
			continue;
		}
		current.push(line);
	}
	if (current.length > 0) {
		hunks.push([...headerLines, ...current].join("\n"));
	}
	return hunks;
}

function fileToReviewFile(
	file: FileDiffMetadata,
	rawPatch: string,
	index: number,
): ReviewFile {
	const noteKey = `${file.prevName ?? ""}->${file.name}`;
	const rawHunks = splitRawPatchIntoHunks(rawPatch);
	let renderedStartLine = 0;
	const hunks = file.hunks.map((hunk, hunkIndex) => {
		const reviewHunk = hunkToReviewHunk(
			file,
			hunk,
			noteKey,
			hunkIndex,
			renderedStartLine,
			rawHunks[hunkIndex] ?? rawPatch,
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

export async function loadReviewFiles(cwd?: string): Promise<ReviewFile[]> {
	const diff = getWorkingTreeDiff(cwd);
	if (!diff.trim()) return [];
	const parsed = parsePatchFiles(diff, "review", true);
	const rawFiles = splitRawDiffIntoFiles(diff);
	const files = parsed.flatMap((patch) => patch.files);
	return files.map((file, index) =>
		fileToReviewFile(file, rawFiles[index] ?? "", index),
	);
}
