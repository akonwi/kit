import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import type { FileDiffMetadata, Hunk as PierreHunk } from "@pierre/diffs";
import { parsePatchFiles } from "@pierre/diffs";
import { safeProcessCwd } from "../../process-cwd";
import type { ReviewHunk, ReviewLine } from "../../shell/diff/types";
import { inferFiletype } from "../../shell/filetype";

export type { ReviewHunk, ReviewLine } from "../../shell/diff/types";

export type ReviewDiffSource = "working" | "untracked";

export type ReviewSkippedSection = {
	id: string;
	beforeHunkIndex: number;
	rawPatch: string;
	lineCount: number;
	additionStart: number;
	deletionStart: number;
};

export type ReviewFile = {
	id: string;
	noteKey: string;
	path: string;
	prevPath?: string;
	status: FileDiffMetadata["type"];
	source: ReviewDiffSource;
	filetype?: string;
	rawPatch: string;
	hunks: ReviewHunk[];
	skippedSections: ReviewSkippedSection[];
	changeCount: number;
	unifiedLineCount: number;
	splitLineCount: number;
};

function runGit(
	cwd: string | undefined,
	args: string[],
	errorMessage: string,
): string {
	const result = spawnSync("git", args, {
		encoding: "utf8",
		cwd: cwd || safeProcessCwd(),
	});
	if (result.status !== 0) {
		throw new Error(result.stderr || errorMessage);
	}
	return result.stdout;
}

function tryRunGit(cwd: string | undefined, args: string[]): string | null {
	const result = spawnSync("git", args, {
		encoding: "utf8",
		cwd: cwd || safeProcessCwd(),
	});
	if (result.status !== 0) return null;
	return result.stdout;
}

/**
 * Diff from HEAD to working tree — covers staged and unstaged changes in a
 * single patch. The review intentionally does not distinguish between the
 * two; users just want to see what they've changed since HEAD. A side
 * benefit is that each path appears at most once, so the file tree can't
 * receive duplicate entries for the same path.
 */
function getWorkingTreeDiff(cwd?: string): string {
	return runGit(
		cwd,
		[
			"diff",
			"HEAD",
			"--no-ext-diff",
			"--find-renames",
			"--find-copies",
			"--unified=3",
		],
		"Failed to read working tree diff.",
	);
}

function getUntrackedDiff(cwd?: string): string {
	const output = runGit(
		cwd,
		["ls-files", "--others", "--exclude-standard", "-z"],
		"Failed to list untracked files.",
	);
	const paths = output.split("\0").filter(Boolean);
	if (paths.length === 0) return "";
	const effectiveCwd = cwd || safeProcessCwd();
	const repoRoot = runGit(
		cwd,
		["rev-parse", "--show-toplevel"],
		"Failed to resolve repository root.",
	).trim();
	return paths
		.map((filePath) => {
			// ls-files returns paths relative to cwd; resolve to repo-relative
			const absPath = path.resolve(effectiveCwd, filePath);
			const repoRelative = path.relative(repoRoot, absPath);
			return buildUntrackedFilePatch(repoRoot, repoRelative);
		})
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

	const lines = splitFileLines(content);
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

function splitFileLines(content: string): string[] {
	const normalized = content.replace(/\r\n/g, "\n");
	if (normalized.length === 0) return [];
	const lines = normalized.split("\n");
	if (lines[lines.length - 1] === "") lines.pop();
	return lines;
}

function readWorkingTreeLines(
	repoRoot: string,
	relativePath: string,
): string[] | null {
	const absolutePath = path.join(repoRoot, relativePath);
	if (!existsSync(absolutePath)) return null;
	try {
		return splitFileLines(readFileSync(absolutePath, "utf8"));
	} catch {
		return null;
	}
}

function readGitRevisionLines(
	cwd: string | undefined,
	revision: string,
	relativePath: string,
): string[] | null {
	const output = tryRunGit(cwd, ["show", `${revision}:${relativePath}`]);
	if (output === null) return null;
	return splitFileLines(output);
}

function loadDisplayLines(options: {
	cwd?: string;
	repoRoot: string;
	file: FileDiffMetadata;
	source: ReviewDiffSource;
}): string[] {
	const beforePath = options.file.prevName ?? options.file.name;
	const afterPath = options.file.name;

	switch (options.source) {
		case "working": {
			const afterLines =
				options.file.type === "deleted"
					? null
					: readWorkingTreeLines(options.repoRoot, afterPath);
			const beforeLines =
				options.file.type === "new"
					? null
					: readGitRevisionLines(options.cwd, "HEAD", beforePath);
			return afterLines ?? beforeLines ?? [];
		}
		case "untracked":
			return readWorkingTreeLines(options.repoRoot, afterPath) ?? [];
	}
}

function buildReviewLinesFromPierreHunk(
	file: FileDiffMetadata,
	hunk: PierreHunk,
): ReviewLine[] {
	const lines: ReviewLine[] = [];
	let nextAdditionLineNumber = hunk.additionStart;
	let nextDeletionLineNumber = hunk.deletionStart;
	for (const block of hunk.hunkContent) {
		if (block.type === "context") {
			for (let index = 0; index < block.lines; index += 1) {
				lines.push({
					kind: "context",
					text: file.additionLines[block.additionLineIndex + index] ?? "",
					additionLineNumber: nextAdditionLineNumber + index,
					deletionLineNumber: nextDeletionLineNumber + index,
				});
			}
			nextAdditionLineNumber += block.lines;
			nextDeletionLineNumber += block.lines;
			continue;
		}
		for (let index = 0; index < block.deletions; index += 1) {
			lines.push({
				kind: "delete",
				text: file.deletionLines[block.deletionLineIndex + index] ?? "",
				deletionLineNumber: nextDeletionLineNumber + index,
			});
		}
		for (let index = 0; index < block.additions; index += 1) {
			lines.push({
				kind: "add",
				text: file.additionLines[block.additionLineIndex + index] ?? "",
				additionLineNumber: nextAdditionLineNumber + index,
			});
		}
		nextDeletionLineNumber += block.deletions;
		nextAdditionLineNumber += block.additions;
	}
	return lines;
}

function hunkToReviewHunk(
	file: FileDiffMetadata,
	hunk: PierreHunk,
	fileNoteKey: string,
	index: number,
	rawPatch: string,
): ReviewHunk {
	let cachedLines: ReviewLine[] | null = null;
	const noteKey = `${fileNoteKey}:${hunk.hunkSpecs ?? `hunk-${index + 1}`}:${index}`;
	const header = hunk.hunkSpecs ?? `Hunk ${index + 1}`;
	return {
		id: `${file.name}:${hunk.hunkSpecs ?? index}:${index}`,
		noteKey,
		header,
		context: hunk.hunkContext ?? "",
		get lines() {
			cachedLines ??= buildReviewLinesFromPierreHunk(file, hunk);
			return cachedLines;
		},
		changeCount: hunk.additionLines + hunk.deletionLines,
		rawPatch,
		patchStartLine: hunk.unifiedLineStart,
		patchLineCount: hunk.unifiedLineCount,
		additionStart: hunk.additionStart,
		additionCount: hunk.additionCount,
		deletionStart: hunk.deletionStart,
		deletionCount: hunk.deletionCount,
		collapsedBefore: hunk.collapsedBefore,
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

function extractRawPatchHeader(rawPatch: string): string[] {
	const lines = rawPatch.replace(/\r\n/g, "\n").split("\n");
	const firstHunkIndex = lines.findIndex((line) => line.startsWith("@@ "));
	if (firstHunkIndex < 0) return lines;
	return lines.slice(0, firstHunkIndex);
}

function formatHunkSpan(start: number, count: number): string {
	return count === 1 ? `${start}` : `${start},${count}`;
}

function buildSkippedSectionPatch(
	headerLines: string[],
	lines: string[],
	additionStart: number,
	deletionStart: number,
): string {
	const count = lines.length;
	return [
		...headerLines,
		`@@ -${formatHunkSpan(deletionStart, count)} +${formatHunkSpan(additionStart, count)} @@`,
		...lines.map((line) => ` ${line}`),
	].join("\n");
}

export function buildSkippedSectionsForFile(
	fileId: string,
	rawPatch: string,
	hunks: ReviewHunk[],
	displayLines: string[],
): ReviewSkippedSection[] {
	if (hunks.length === 0 || displayLines.length === 0) return [];
	const headerLines = extractRawPatchHeader(rawPatch);
	const sections: ReviewSkippedSection[] = [];

	for (const [index, hunk] of hunks.entries()) {
		if (hunk.collapsedBefore <= 0) continue;
		const additionStart = hunk.additionStart - hunk.collapsedBefore;
		const deletionStart = hunk.deletionStart - hunk.collapsedBefore;
		const displayStart = additionStart > 0 ? additionStart : deletionStart;
		const gapLines = displayLines.slice(
			Math.max(0, displayStart - 1),
			Math.max(0, displayStart - 1) + hunk.collapsedBefore,
		);
		if (gapLines.length === 0) continue;
		sections.push({
			id: `${fileId}:gap:${index}`,
			beforeHunkIndex: index,
			rawPatch: buildSkippedSectionPatch(
				headerLines,
				gapLines,
				additionStart,
				deletionStart,
			),
			lineCount: gapLines.length,
			additionStart,
			deletionStart,
		});
	}

	const lastHunk = hunks[hunks.length - 1];
	const trailingAdditionStart = lastHunk.additionStart + lastHunk.additionCount;
	const trailingDeletionStart = lastHunk.deletionStart + lastHunk.deletionCount;
	const trailingDisplayStart =
		trailingAdditionStart > 0 ? trailingAdditionStart : trailingDeletionStart;
	const trailingLines = displayLines.slice(
		Math.max(0, trailingDisplayStart - 1),
	);
	if (trailingLines.length > 0) {
		sections.push({
			id: `${fileId}:gap:${hunks.length}`,
			beforeHunkIndex: hunks.length,
			rawPatch: buildSkippedSectionPatch(
				headerLines,
				trailingLines,
				trailingAdditionStart,
				trailingDeletionStart,
			),
			lineCount: trailingLines.length,
			additionStart: trailingAdditionStart,
			deletionStart: trailingDeletionStart,
		});
	}

	return sections;
}

const EAGER_SKIPPED_SECTIONS_FILE_LIMIT = 50;

function fileToReviewFile(
	file: FileDiffMetadata,
	rawPatch: string,
	index: number,
	options: {
		cwd?: string;
		repoRoot: string;
		source: ReviewDiffSource;
		includeSkippedSections: boolean;
	},
): ReviewFile {
	const noteKey = `${options.source}:${file.prevName ?? ""}->${file.name}`;
	const rawHunks = splitRawPatchIntoHunks(rawPatch);
	const hunks = file.hunks.map((hunk, hunkIndex) =>
		hunkToReviewHunk(
			file,
			hunk,
			noteKey,
			hunkIndex,
			rawHunks[hunkIndex] ?? rawPatch,
		),
	);
	const changeCount = hunks.reduce((sum, hunk) => sum + hunk.changeCount, 0);
	const id = `${noteKey}:${index}`;
	const skippedSections = options.includeSkippedSections
		? buildSkippedSectionsForFile(
				id,
				rawPatch,
				hunks,
				loadDisplayLines({
					cwd: options.cwd,
					repoRoot: options.repoRoot,
					file,
					source: options.source,
				}),
			)
		: [];
	return {
		id,
		noteKey,
		path: file.name,
		prevPath: file.prevName,
		status: file.type,
		source: options.source,
		filetype: inferFiletype(file.name),
		rawPatch,
		hunks,
		skippedSections,
		changeCount,
		unifiedLineCount: file.unifiedLineCount,
		splitLineCount: file.splitLineCount,
	};
}

type ReviewPatchSet = {
	source: ReviewDiffSource;
	files: FileDiffMetadata[];
	rawFiles: string[];
};

function parseReviewPatchSet(
	diff: string,
	source: ReviewDiffSource,
): ReviewPatchSet | null {
	if (!diff.trim()) return null;
	const parsed = parsePatchFiles(diff, "review", true);
	return {
		source,
		files: parsed.flatMap((patch) => patch.files),
		rawFiles: splitRawDiffIntoFiles(diff),
	};
}

function yieldToRenderer(): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, 0));
}

export async function loadReviewFiles(cwd?: string): Promise<ReviewFile[]> {
	await yieldToRenderer();
	const working = getWorkingTreeDiff(cwd);
	const untracked = getUntrackedDiff(cwd);
	const patchSets = [
		parseReviewPatchSet(working, "working"),
		parseReviewPatchSet(untracked, "untracked"),
	].filter((value): value is ReviewPatchSet => value !== null);
	if (patchSets.length === 0) return [];

	const repoRoot = runGit(
		cwd,
		["rev-parse", "--show-toplevel"],
		"Failed to resolve repository root.",
	).trim();
	const totalFileCount = patchSets.reduce(
		(count, patchSet) => count + patchSet.files.length,
		0,
	);
	const includeSkippedSections =
		totalFileCount <= EAGER_SKIPPED_SECTIONS_FILE_LIMIT;
	const reviewFiles: ReviewFile[] = [];
	for (const patchSet of patchSets) {
		for (const [index, file] of patchSet.files.entries()) {
			reviewFiles.push(
				fileToReviewFile(
					file,
					patchSet.rawFiles[index] ?? "",
					reviewFiles.length,
					{
						cwd,
						repoRoot,
						source: patchSet.source,
						includeSkippedSections,
					},
				),
			);
		}
	}
	return reviewFiles;
}

/** Resolve the git repository root for the given working directory. */
export function getRepoRoot(cwd?: string): string {
	return runGit(
		cwd,
		["rev-parse", "--show-toplevel"],
		"Failed to resolve repository root.",
	).trim();
}

/** List all tracked files in the repo via `git ls-files`. */
export function listRepoFiles(cwd?: string): string[] {
	const result = spawnSync("git", ["ls-files", "--full-name"], {
		cwd: cwd || safeProcessCwd(),
		encoding: "utf8",
		maxBuffer: 10 * 1024 * 1024,
	});
	if (result.status !== 0) return [];
	return result.stdout.trim().split("\n").filter(Boolean);
}
